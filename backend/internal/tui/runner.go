package tui

import (
	"bufio"
	"context"
	"log"
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
	go r.runTask(task, cfg.EnvExtra)
	return task, nil
}

func (r *Runner) StopTask(task *db.Task) {
	if task.ContainerID != "" {
		_ = r.docker.StopContainer(context.Background(), task.ContainerID)
	}
	_ = r.db.UpdateTaskStatus(task.ID, "stopped", task.ContainerID)
}

func (r *Runner) runTask(task *db.Task, envExtra []string) {
	ctx := context.Background()

	containerID, err := r.docker.RunTask(ctx, docker.RunConfig{
		TaskID:       task.ID,
		RepoURL:      task.RepoURL,
		RepoPath:     task.RepoPath,
		RepoBranch:   task.RepoBranch,
		AgentCommand: task.AgentCommand,
		EnvExtra:     envExtra,
	})
	if err != nil {
		log.Printf("task %s: run error: %v", task.ID, err)
		_ = r.db.UpdateTaskStatus(task.ID, "error", "")
		return
	}

	_ = r.db.UpdateTaskStatus(task.ID, "running", containerID)
	log.Printf("task %s: container started: %s", task.ID, containerID[:12])

	go r.watchForDoneSignal(task, containerID)

	exitCode, err := r.docker.WaitContainer(ctx, containerID)
	log.Printf("task %s: container exited exit=%d", task.ID, exitCode)

	if cur, _ := r.db.GetTask(task.ID); cur != nil && cur.Status == "running" {
		finalStatus := "error"
		if err == nil && exitCode == 0 {
			finalStatus = "idle"
		}
		_ = r.db.UpdateTaskStatus(task.ID, finalStatus, containerID)
		if story, sErr := r.db.GetStoryByTaskID(task.ID); sErr == nil && story.KanbanStatus == "doing" {
			_ = r.db.UpdateStoryKanban(story.ID, "review", story.TaskID)
		}
	}

	time.AfterFunc(2*time.Hour, func() {
		_ = r.docker.RemoveContainer(context.Background(), containerID)
	})
}

func (r *Runner) watchForDoneSignal(task *db.Task, containerID string) {
	ctx := context.Background()
	stream, err := r.docker.StreamLogs(ctx, containerID)
	if err != nil {
		log.Printf("task %s: watchForDoneSignal: %v", task.ID, err)
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		if !strings.Contains(scanner.Text(), "[QUINOA:DONE]") {
			continue
		}
		story, err := r.db.GetStoryByTaskID(task.ID)
		if err != nil || story.KanbanStatus != "doing" {
			continue
		}
		_ = r.db.UpdateTaskStatus(task.ID, "idle", containerID)
		_ = r.db.UpdateStoryKanban(story.ID, "review", story.TaskID)
		log.Printf("task %s: agent signalled completion → review", task.ID)
	}
}
