package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/wiltsou/quinoa/internal/docker"
)

// detachSeq is the two-byte sequence that detaches from the container without
// killing the agent: Ctrl+B (0x02) followed by 'd'. Same convention as tmux.
var detachSeq = [2]byte{0x02, 'd'}

// attachExec implements tea.ExecCommand.
// When Run() is called by bubbletea (after it releases the terminal),
// it sets raw mode, streams buffered logs, then opens a live interactive
// attach to the container PTY. Returns when the container exits or the
// user presses the detach sequence (Ctrl+B d).
type attachExec struct {
	containerID string
	docker      *docker.Client

	// set by bubbletea before Run()
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func newAttachExec(containerID string, d *docker.Client) *attachExec {
	return &attachExec{containerID: containerID, docker: d}
}

func (a *attachExec) SetStdin(r io.Reader)  { a.stdin = r }
func (a *attachExec) SetStdout(w io.Writer) { a.stdout = w }
func (a *attachExec) SetStderr(w io.Writer) { a.stderr = w }

func (a *attachExec) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// bubbletea passes its internal input reader; extract the real fd for
	// raw-mode and size-query operations.
	fd := termFD(a.stdin)

	// Put terminal into raw mode so every keystroke goes straight to the
	// container without line-buffering or echo.
	if fd >= 0 && term.IsTerminal(fd) {
		old, err := term.MakeRaw(fd)
		if err == nil {
			defer term.Restore(fd, old) //nolint:errcheck
		}
		if w, h, err := term.GetSize(fd); err == nil {
			_ = a.docker.ResizeTerminal(ctx, a.containerID, uint(h), uint(w))
		}
	}

	// Propagate window-resize signals so the agent sees the correct size.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	go func() {
		for range sigCh {
			if fd >= 0 {
				if w, h, err := term.GetSize(fd); err == nil {
					_ = a.docker.ResizeTerminal(ctx, a.containerID, uint(h), uint(w))
				}
			}
		}
	}()
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()

	// Banner (written before live PTY output floods the screen).
	fmt.Fprintf(a.stdout, "\r\n\033[2m[quinoa] sessão interativa — Ctrl+B d para desanexar\033[0m\r\n\r\n")

	// Replay buffered history so the user doesn't land in a blank screen.
	if logs, err := a.docker.GetLogs(ctx, a.containerID); err == nil {
		_, _ = io.Copy(a.stdout, logs)
		logs.Close()
	}

	// Open bidirectional live attach to the container PTY.
	attach, err := a.docker.AttachTerminal(ctx, a.containerID)
	if err != nil {
		fmt.Fprintf(a.stdout, "\r\n\033[31m[quinoa] erro ao conectar: %v\033[0m\r\n", err)
		return nil
	}
	defer attach.Close()

	outputDone := make(chan struct{})

	// Container PTY → terminal stdout.
	go func() {
		defer close(outputDone)
		_, _ = io.Copy(a.stdout, attach.Reader)
	}()

	// Terminal stdin → container PTY, intercepting the detach sequence.
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		buf := make([]byte, 256)
		seqState := 0 // 0 = normal, 1 = saw Ctrl+B
		for {
			n, err := a.stdin.Read(buf)
			if err != nil {
				return
			}
			var toSend []byte
			for _, b := range buf[:n] {
				switch seqState {
				case 0:
					if b == detachSeq[0] {
						seqState = 1 // buffer Ctrl+B, wait for 'd'
					} else {
						toSend = append(toSend, b)
					}
				case 1:
					seqState = 0
					if b == detachSeq[1] {
						cancel() // detach without killing the container
						return
					}
					// False alarm — emit the buffered Ctrl+B plus this byte.
					toSend = append(toSend, detachSeq[0], b)
				}
			}
			if len(toSend) > 0 {
				if _, err := attach.Conn.Write(toSend); err != nil {
					return
				}
			}
		}
	}()

	// Block until the container exits, the user detaches, or stdin closes.
	select {
	case <-outputDone:
	case <-inputDone:
	case <-ctx.Done():
	}

	fmt.Fprintf(a.stdout, "\r\n\033[2m[quinoa] desanexado\033[0m\r\n")
	return nil
}

// termFD returns the file descriptor of r if r is an *os.File, otherwise -1.
func termFD(r io.Reader) int {
	if f, ok := r.(*os.File); ok {
		return int(f.Fd())
	}
	// Fallback: try os.Stdin directly (bubbletea passes os.Stdin under the hood).
	return int(os.Stdin.Fd())
}
