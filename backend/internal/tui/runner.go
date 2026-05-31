package tui

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
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
		StoryID:      cfg.StoryID,
		RepoURL:      cfg.RepoURL,
		RepoPath:     cfg.RepoPath,
		RepoBranch:   cfg.RepoBranch,
		AgentCommand: cfg.AgentCommand,
		BaseCommand:  cfg.BaseCommand,
		Status:       "pending",
	}
	if err := r.db.InsertTask(task); err != nil {
		return nil, err
	}
	go r.runTask(cfg)
	return task, nil
}

func (r *Runner) StopTask(task *db.Task) {
	// Update DB before stopping so runTask's WaitContainer sees "stopped" and
	// doesn't race to overwrite the final status to "error".
	_ = r.db.UpdateTaskStatus(task.ID, "stopped", task.ContainerID)
	if task.ContainerID != "" {
		_ = r.docker.StopContainer(context.Background(), task.ContainerID)
	}
}

// StopStoryTasks stops all running or pending tasks linked to the given story.
func (r *Runner) StopStoryTasks(storyID string) {
	tasks, _ := r.db.ListTasksByStory(storyID)
	for _, t := range tasks {
		if t.Status == "running" || t.Status == "pending" {
			r.StopTask(t)
		}
	}
}

// getStoryForTask finds the story for a task. It checks both the primary task_id
// link (story.task_id = taskID) and the task's own story_id field, so spawned
// parallel agents can also find their story.
func (r *Runner) getStoryForTask(taskID string) (*db.Story, error) {
	if story, err := r.db.GetStoryByTaskID(taskID); err == nil {
		return story, nil
	}
	task, err := r.db.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if task.StoryID == "" {
		return nil, fmt.Errorf("task %s has no story_id", taskID)
	}
	return r.db.GetStory(task.StoryID)
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
		if story, sErr := r.getStoryForTask(taskID); sErr == nil && story.KanbanStatus == "doing" {
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
		line := scanner.Text()

		// Spawn signal: [QUINOA:SPAWN:<instruction>]
		// Only valid while the story is in "doing". The agent (started from refine→doing)
		// emits this after analyzing the PRD to request a parallel sub-agent.
		if idx := strings.Index(line, "[QUINOA:SPAWN:"); idx != -1 {
			end := strings.Index(line[idx:], "]")
			if end > 0 {
				instruction := line[idx+len("[QUINOA:SPAWN:") : idx+end]
				instruction = strings.TrimSpace(instruction)
				if instruction != "" {
					r.spawnParallelAgent(taskID, instruction)
				}
			}
			continue
		}

		if !strings.Contains(line, "[QUINOA:DONE]") {
			continue
		}
		story, err := r.getStoryForTask(taskID)
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
			vaultPath, _ := r.db.GetConfig(db.ConfigVaultPath)
			triggerVaultUpdate(vaultPath, story.Title, story.Description, story.PrdPath, "Refinamento concluído")
		}
	}
}

// spawnParallelAgent creates a new parallel agent for the same story.
// Called when the primary doing-agent emits [QUINOA:SPAWN:<instruction>].
func (r *Runner) spawnParallelAgent(parentTaskID, instruction string) {
	parent, err := r.db.GetTask(parentTaskID)
	if err != nil || parent.BaseCommand == "" || parent.StoryID == "" {
		log.Printf("task %s: SPAWN ignored — missing base_command or story_id", parentTaskID)
		return
	}
	story, err := r.db.GetStory(parent.StoryID)
	if err != nil || story.KanbanStatus != "doing" {
		log.Printf("task %s: SPAWN ignored — story not in doing", parentTaskID)
		return
	}

	vaultPath, _ := r.db.GetConfig(db.ConfigVaultPath)
	projectsPath, _ := r.db.GetConfig(db.ConfigProjectsPath)

	containerPRD := ""
	if vaultPath != "" && story.PrdPath != "" {
		if rel := strings.TrimPrefix(story.PrdPath, vaultPath); rel != story.PrdPath {
			containerPRD = "/vault" + rel
		}
	}

	agentCmd := parent.BaseCommand + " " + shellQuote(instruction)
	if containerPRD != "" {
		agentCmd = parent.BaseCommand + " " + shellQuote(instruction+"\n\nPRD disponível em: "+containerPRD)
	}

	taskID := uuid.New().String()
	cfg := docker.RunConfig{
		TaskID:       taskID,
		StoryID:      parent.StoryID,
		AgentCommand: agentCmd,
		BaseCommand:  parent.BaseCommand,
		VaultPath:    vaultPath,
		ProjectsPath: projectsPath,
	}
	if _, err := r.StartTask(cfg); err != nil {
		log.Printf("task %s: SPAWN failed: %v", parentTaskID, err)
		return
	}
	log.Printf("task %s: spawned parallel agent %s", parentTaskID, taskID)
}
