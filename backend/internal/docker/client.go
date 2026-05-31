package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	dtypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	dockerclient "github.com/docker/docker/client"
)

const sandboxImage = "quinoa-sandbox"

type Client struct {
	cli *dockerclient.Client
}

type RunConfig struct {
	TaskID       string
	StoryID      string
	RepoURL      string
	RepoPath     string // local path to bind-mount at /workspace/repo (optional)
	RepoBranch   string
	AgentCommand string
	BaseCommand  string   // agent binary + flags without the prompt (e.g. "claude --dangerously-skip-permissions")
	EnvExtra     []string
	VaultPath    string // Obsidian vault — bind-mounted at /vault (optional)
	ProjectsPath string // local projects root — bind-mounted at /projects (optional)
}

func NewClient() (*Client, error) {
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Client{cli: cli}, nil
}

// RunTask creates and starts a sandboxed container for the given task.
// The container is created with a PTY (Tty=true) so the agent can be
// attached as an interactive terminal via AttachTerminal.
func (c *Client) RunTask(ctx context.Context, cfg RunConfig) (string, error) {
	env := []string{
		"AGENT_COMMAND=" + cfg.AgentCommand,
		"REPO_URL=" + cfg.RepoURL,
		"REPO_BRANCH=" + cfg.RepoBranch,
		"TERM=xterm-256color",
	}
	env = append(env, cfg.EnvExtra...)

	hostCfg := &container.HostConfig{NetworkMode: "bridge"}
	if cfg.RepoPath != "" {
		hostCfg.Mounts = []mount.Mount{{
			Type:   mount.TypeBind,
			Source: cfg.RepoPath,
			Target: "/workspace/repo",
		}}
	}

	if cfg.VaultPath != "" {
		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: cfg.VaultPath,
			Target: "/vault",
		})
	}

	if cfg.ProjectsPath != "" {
		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: cfg.ProjectsPath,
			Target: "/projects",
		})
	}

	// Mount host Claude credentials read-only into a staging directory.
	// runner.sh copies them to $HOME before starting the agent so each
	// container has its own writable copy and concurrent agents don't conflict.
	if home := os.Getenv("HOME"); home != "" {
		claudeDir := filepath.Join(home, ".claude")
		if info, err := os.Stat(claudeDir); err == nil && info.IsDir() {
			hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   claudeDir,
				Target:   "/run/claude-host-creds/claude",
				ReadOnly: true,
			})
		}
		claudeJSON := filepath.Join(home, ".claude.json")
		if _, err := os.Stat(claudeJSON); err == nil {
			hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   claudeJSON,
				Target:   "/run/claude-host-creds/claude.json",
				ReadOnly: true,
			})
		}
	}

	// Propagate API key from host environment if set.
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		env = append(env, "ANTHROPIC_API_KEY="+key)
	}

	resp, err := c.cli.ContainerCreate(ctx,
		&container.Config{
			Image:        sandboxImage,
			Env:          env,
			Tty:          true,  // allocate a PTY
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			OpenStdin:    true,  // keep stdin open for interactive input
			Labels:       map[string]string{"quinoa.task_id": cfg.TaskID},
		},
		hostCfg,
		nil, nil,
		"quinoa-"+cfg.TaskID,
	)
	if err != nil {
		return "", fmt.Errorf("container create: %w", err)
	}

	if err := c.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("container start: %w", err)
	}
	return resp.ID, nil
}

// AttachTerminal opens a bidirectional raw stream to the container's PTY.
// With Tty=true the stream is raw (no Docker multiplexing header).
// The caller is responsible for closing the returned HijackedResponse.
func (c *Client) AttachTerminal(ctx context.Context, containerID string) (dtypes.HijackedResponse, error) {
	return c.cli.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
}

// GetLogs returns a snapshot of the container's buffered PTY output (non-blocking).
// Used to replay history to a WebSocket client before starting the live interactive attach.
func (c *Client) GetLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
	})
}

// StreamLogs returns a continuous (follow) stream of the container's PTY output.
// The caller is responsible for closing the returned ReadCloser.
func (c *Client) StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
}

// ResizeTerminal sends SIGWINCH to the container process with the new dimensions.
func (c *Client) ResizeTerminal(ctx context.Context, containerID string, rows, cols uint) error {
	return c.cli.ContainerResize(ctx, containerID, container.ResizeOptions{
		Height: rows,
		Width:  cols,
	})
}

// WaitContainer blocks until the container stops and returns its exit code.
func (c *Client) WaitContainer(ctx context.Context, containerID string) (int64, error) {
	resultC, errC := c.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case res := <-resultC:
		return res.StatusCode, nil
	case err := <-errC:
		return -1, err
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

func (c *Client) StopContainer(ctx context.Context, containerID string) error {
	timeout := 10
	return c.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

func (c *Client) RemoveContainer(ctx context.Context, containerID string) error {
	return c.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}
