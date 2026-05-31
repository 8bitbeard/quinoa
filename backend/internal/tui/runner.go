package tui

import (
	"bufio"
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/wiltsou/quinoa/internal/db"
	"github.com/wiltsou/quinoa/internal/docker"
)

// Runner holds shared dependencies for executing and monitoring tasks.
type Runner struct {
	db     *db.DB
	docker *docker.Client
}

func NewRunner(database *db.DB, dockerClient *docker.Client) *Runner {
	return &Runner{db: database, docker: dockerClient}
}

func (r *Runner) Docker() *docker.Client { return r.docker }

// StartTask creates a task record and launches the container in the background.
// Returns the created task (with ID) so the caller can link it to a story.
func (r *Runner) StartTask(cfg docker.RunConfig) (*db.Task, error) {
	task := &db.Task{
		ID:           cfg.TaskID,
		RepoURL:      cfg.RepoURL,
		RepoPath:     cfg.RepoPath,
		RepoBranch:   cfg.RepoBranch,
		AgentCommand: cfg.AgentCommand,
		Status:       "pending",
	}
	if err := r.db.InsertTask(task); err != nil {
		return nil, err
	}
	go r.runTask(cfg)
	return task, nil
}

func (r *Runner) StopTask(task *db.Task) {
	if task.ContainerID != "" {
		_ = r.docker.StopContainer(context.Background(), task.ContainerID)
	}
	_ = r.db.UpdateTaskStatus(task.ID, "stopped", task.ContainerID)
}

func (r *Runner) runTask(cfg docker.RunConfig) {
	ctx := context.Background()
	taskID := cfg.TaskID

	containerID, err := r.docker.RunTask(ctx, cfg)
	if err != nil {
		log.Printf("task %s: run error: %v", taskID, err)
		_ = r.db.UpdateTaskStatus(taskID, "error", "")
		return
	}

	_ = r.db.UpdateTaskStatus(taskID, "running", containerID)
	log.Printf("task %s: container started: %s", taskID, containerID[:12])

	go r.watchForDoneSignal(taskID, containerID)

	exitCode, err := r.docker.WaitContainer(ctx, containerID)
	log.Printf("task %s: container exited exit=%d", taskID, exitCode)

	if cur, _ := r.db.GetTask(taskID); cur != nil && cur.Status == "running" {
		finalStatus := "error"
		if err == nil && exitCode == 0 {
			finalStatus = "idle"
		}
		_ = r.db.UpdateTaskStatus(taskID, finalStatus, containerID)
		if story, sErr := r.db.GetStoryByTaskID(taskID); sErr == nil && story.KanbanStatus == "doing" {
			_ = r.db.UpdateStoryKanban(story.ID, "review", story.TaskID)
		}
	}

	time.AfterFunc(2*time.Hour, func() {
		_ = r.docker.RemoveContainer(context.Background(), containerID)
	})
}

// verifyPRD checks that the PRD file was actually written by the refinement agent.
// It logs the result; callers should not treat a missing PRD as fatal since the
// agent may have printed it to stdout instead (no-vault mode).
func (r *Runner) verifyPRD(taskID, prdPath string) {
	if prdPath == "" {
		log.Printf("task %s: no PRD path configured", taskID)
		return
	}
	info, err := os.Stat(prdPath)
	if err != nil {
		log.Printf("task %s: WARNING — PRD not found at %s: %v", taskID, prdPath, err)
		return
	}
	if info.Size() == 0 {
		log.Printf("task %s: WARNING — PRD file is empty at %s", taskID, prdPath)
		return
	}
	log.Printf("task %s: PRD verified (%d bytes): %s", taskID, info.Size(), prdPath)
}

func (r *Runner) watchForDoneSignal(taskID, containerID string) {
	ctx := context.Background()
	stream, err := r.docker.StreamLogs(ctx, containerID)
	if err != nil {
		log.Printf("task %s: watchForDoneSignal: %v", taskID, err)
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		if !strings.Contains(scanner.Text(), "[QUINOA:DONE]") {
			continue
		}
		story, err := r.db.GetStoryByTaskID(taskID)
		if err != nil {
			continue
		}
		switch story.KanbanStatus {
		case "doing":
			_ = r.db.UpdateTaskStatus(taskID, "idle", containerID)
			_ = r.db.UpdateStoryKanban(story.ID, "review", story.TaskID)
			log.Printf("task %s: agent signalled completion → review", taskID)
		case "refine":
			// Task goes idle; story stays in refine so the user can review the PRD before promoting.
			_ = r.db.UpdateTaskStatus(taskID, "idle", containerID)
			r.verifyPRD(taskID, story.PrdPath)
			log.Printf("task %s: refinement agent signalled done → PRD ready", taskID)
		}
	}
}
