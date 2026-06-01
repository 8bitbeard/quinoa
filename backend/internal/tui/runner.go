package tui

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
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
		AgentName:    cfg.AgentName,
		Stage:        cfg.Stage,
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

	// watchForDoneSignal runs concurrently processing [QUINOA:SPAWN:] and [QUINOA:DONE]
	// signals from the log stream. We must wait for it to finish before checking
	// AllStoryTasksDone, otherwise we may transition before spawned agents are in the DB.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.watchForDoneSignal(taskID, containerID)
	}()

	exitCode, err := r.docker.WaitContainer(ctx, containerID)
	log.Printf("task %s: container exited exit=%d", taskID, exitCode)

	// Wait for the log watcher to finish processing all signals before
	// making any transition decision. The Docker log stream closes when
	// the container exits, so this should return promptly.
	wg.Wait()

	if cur, _ := r.db.GetTask(taskID); cur != nil && cur.Status == "running" {
		// watchForDoneSignal did not see [QUINOA:DONE] — the container exited without signaling.
		finalStatus := "error"
		if err == nil && exitCode == 0 {
			finalStatus = "idle"
		}
		_ = r.db.UpdateTaskStatus(taskID, finalStatus, containerID)

		story, sErr := r.getStoryForTask(taskID)
		if sErr == nil {
			switch story.KanbanStatus {
			case "doing":
				if r.db.AllStoryTasksDone(story.ID) {
					r.transitionToReview(story)
				}
			case "review":
				if finalStatus == "error" {
					_ = r.db.SetReviewResult(story.ID, "issues")
				}
				if r.db.AllStoryTasksDone(story.ID) {
					r.handleReviewComplete(story.ID)
				}
			}
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
	// Increase buffer to handle long lines (agent output can be verbose).
	scanner.Buffer(make([]byte, 512*1024), 512*1024)

	var (
		spawnBuf strings.Builder
		inSpawn  bool // true while accumulating a multi-line [QUINOA:SPAWN]...[/QUINOA:SPAWN] block
	)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// ── Multi-line SPAWN accumulation ──────────────────────────────────
		// While inside a SPAWN block we must NOT process any other signals —
		// instruction bodies often contain "[QUINOA:DONE]" text that would
		// otherwise trigger a false transition before the agents are registered.
		if inSpawn {
			if trimmed == "[/QUINOA:SPAWN]" {
				instruction := strings.TrimSpace(spawnBuf.String())
				if instruction != "" {
					r.spawnParallelAgent(taskID, instruction)
				}
				inSpawn = false
				spawnBuf.Reset()
			} else {
				if spawnBuf.Len() > 0 {
					spawnBuf.WriteByte('\n')
				}
				spawnBuf.WriteString(line)
			}
			continue
		}

		// ── New multi-line SPAWN start: [QUINOA:SPAWN] on its own line ─────
		if trimmed == "[QUINOA:SPAWN]" {
			inSpawn = true
			spawnBuf.Reset()
			continue
		}

		// ── Legacy single-line SPAWN: [QUINOA:SPAWN:instruction] ──────────
		// Kept for backward-compatibility.  Only the single-line form is
		// safe because multi-line emission causes false DONE triggers.
		if idx := strings.Index(line, "[QUINOA:SPAWN:"); idx != -1 {
			rest := line[idx+len("[QUINOA:SPAWN:"):]
			if closeIdx := strings.Index(rest, "]"); closeIdx >= 0 {
				instruction := strings.TrimSpace(rest[:closeIdx])
				if instruction != "" {
					r.spawnParallelAgent(taskID, instruction)
				}
			} else {
				// No closing ] on this line — the instruction is multi-line.
				// Enter accumulation mode to avoid false DONE triggers from
				// [QUINOA:DONE] text that may appear inside the instruction body.
				inSpawn = true
				spawnBuf.Reset()
				spawnBuf.WriteString(rest) // everything after [QUINOA:SPAWN:
			}
			continue
		}

		// ── [QUINOA:RETURN_TO_DOING] ───────────────────────────────────────
		if strings.Contains(line, "[QUINOA:RETURN_TO_DOING]") {
			story, sErr := r.getStoryForTask(taskID)
			if sErr == nil && story.KanbanStatus == "review" {
				_ = r.db.SetReviewResult(story.ID, "issues")
				log.Printf("task %s: issues flagged → will return to doing when all review agents finish", taskID)
			}
			continue
		}

		// ── [QUINOA:DONE] ─────────────────────────────────────────────────
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
			if r.db.AllStoryTasksDone(story.ID) {
				r.transitionToReview(story)
			} else {
				log.Printf("task %s: doing agent done, waiting for other agents", taskID)
			}
		case "review":
			_ = r.db.UpdateTaskStatus(taskID, "idle", containerID)
			if r.db.AllStoryTasksDone(story.ID) {
				r.handleReviewComplete(story.ID)
			} else {
				log.Printf("task %s: review agent done, waiting for other agents", taskID)
			}
		case "refine":
			_ = r.db.UpdateTaskStatus(taskID, "idle", containerID)
			r.verifyPRD(taskID, story.PrdPath)
			log.Printf("task %s: PM agent done → PRD ready for promotion", taskID)
			vaultPath, _ := r.db.GetConfig(db.ConfigVaultPath)
			triggerVaultUpdate(vaultPath, story.Title, story.Description, story.PrdPath, "Refinamento concluído")
		}
	}
}

// getBaseCommand returns the agent base command stored in the story's primary task.
// Falls back to "claude --dangerously-skip-permissions" when not available.
func (r *Runner) getBaseCommand(story *db.Story) string {
	if story.TaskID != "" {
		if task, err := r.db.GetTask(story.TaskID); err == nil && task.BaseCommand != "" {
			return task.BaseCommand
		}
	}
	return "claude --dangerously-skip-permissions"
}

// transitionToReview increments the review counter, resets review_result,
// and auto-starts the Tech Lead Review agent.
func (r *Runner) transitionToReview(story *db.Story) {
	_ = r.db.IncrReviewCount(story.ID)
	_ = r.db.SetReviewResult(story.ID, "")
	r.autoStartReviewAgent(story)
	log.Printf("story %s: all doing agents done → starting review", story.ID)
}

// autoStartReviewAgent creates and launches the Tech Lead Review agent.
// It updates story.task_id to the new review task so the card badge reflects it.
func (r *Runner) autoStartReviewAgent(story *db.Story) {
	vaultPath, _ := r.db.GetConfig(db.ConfigVaultPath)
	projectsPath, _ := r.db.GetConfig(db.ConfigProjectsPath)
	baseCmd := r.getBaseCommand(story)

	containerPRD := ""
	if vaultPath != "" && story.PrdPath != "" {
		if rel := strings.TrimPrefix(story.PrdPath, vaultPath); rel != story.PrdPath {
			containerPRD = "/vault" + rel
		}
	}

	hasReviewFeedback := story.ReviewCount > 1 // already been through at least one review cycle
	prompt := buildTechLeadReviewPrompt(story.Title, containerPRD, projectsPath != "", hasReviewFeedback)
	agentCmd := baseCmd + " " + shellQuote(prompt)

	taskID := uuid.New().String()
	cfg := docker.RunConfig{
		TaskID:       taskID,
		StoryID:      story.ID,
		AgentCommand: agentCmd,
		BaseCommand:  baseCmd,
		AgentName:    "TechLead",
		Stage:        "review",
		VaultPath:    vaultPath,
		ProjectsPath: projectsPath,
	}
	task, err := r.StartTask(cfg)
	if err != nil {
		log.Printf("story %s: autoStartReviewAgent failed: %v — moving to review without agent", story.ID, err)
		_ = r.db.UpdateStoryKanban(story.ID, "review", story.TaskID)
		return
	}
	_ = r.db.UpdateStoryKanban(story.ID, "review", task.ID)
	log.Printf("story %s: Tech Lead Review agent started %s", story.ID, task.ID)
}

// handleReviewComplete is called when all review tasks for a story have finished.
// If any agent flagged issues it auto-starts a new doing cycle; otherwise marks as ready.
func (r *Runner) handleReviewComplete(storyID string) {
	story, err := r.db.GetStory(storyID)
	if err != nil {
		return
	}
	if story.ReviewResult == "issues" {
		log.Printf("story %s: review found issues → auto-starting doing agent", storyID)
		r.autoStartDoingAgent(story)
	} else {
		log.Printf("story %s: review passed → ready for human review", storyID)
		_ = r.db.SetReviewResult(storyID, "ready")
	}
}

// autoStartDoingAgent creates and launches a new Tech Lead Doing agent after review issues.
// The story is moved back to "doing" and doing_count is incremented.
func (r *Runner) autoStartDoingAgent(story *db.Story) {
	vaultPath, _ := r.db.GetConfig(db.ConfigVaultPath)
	projectsPath, _ := r.db.GetConfig(db.ConfigProjectsPath)
	baseCmd := r.getBaseCommand(story)

	containerPRD := ""
	if vaultPath != "" && story.PrdPath != "" {
		if rel := strings.TrimPrefix(story.PrdPath, vaultPath); rel != story.PrdPath {
			containerPRD = "/vault" + rel
		}
	}

	prompt := buildTechLeadDoingPrompt(story.Title, story.Description, containerPRD, projectsPath != "", true)
	agentCmd := baseCmd + " " + shellQuote(prompt)

	taskID := uuid.New().String()
	cfg := docker.RunConfig{
		TaskID:       taskID,
		StoryID:      story.ID,
		AgentCommand: agentCmd,
		BaseCommand:  baseCmd,
		AgentName:    "TechLead",
		Stage:        "doing",
		VaultPath:    vaultPath,
		ProjectsPath: projectsPath,
	}
	task, err := r.StartTask(cfg)
	if err != nil {
		log.Printf("story %s: autoStartDoingAgent failed: %v", story.ID, err)
		return
	}
	_ = r.db.UpdateStoryKanban(story.ID, "doing", task.ID)
	_ = r.db.IncrDoingCount(story.ID)
	log.Printf("story %s: Tech Lead Doing agent restarted after review issues %s", story.ID, task.ID)
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
	if err != nil || (story.KanbanStatus != "doing" && story.KanbanStatus != "review") {
		log.Printf("task %s: SPAWN ignored — story not in doing/review (status=%s)", parentTaskID, story.KanbanStatus)
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

	// Inherit stage from parent; derive a human-readable name from the instruction.
	stage := parent.Stage
	if stage == "" {
		stage = story.KanbanStatus
	}
	agentName := deriveAgentName(instruction)

	taskID := uuid.New().String()
	cfg := docker.RunConfig{
		TaskID:       taskID,
		StoryID:      parent.StoryID,
		AgentCommand: agentCmd,
		BaseCommand:  parent.BaseCommand,
		AgentName:    agentName,
		Stage:        stage,
		VaultPath:    vaultPath,
		ProjectsPath: projectsPath,
	}
	if _, err := r.StartTask(cfg); err != nil {
		log.Printf("task %s: SPAWN failed: %v", parentTaskID, err)
		return
	}
	log.Printf("task %s: spawned parallel agent %s (%s)", parentTaskID, taskID, agentName)
}

// deriveAgentName infers a human-readable role label from a spawn instruction.
func deriveAgentName(instruction string) string {
	lower := strings.ToLower(instruction)
	switch {
	case strings.Contains(lower, "agente de qa") || strings.HasPrefix(lower, "você é um agente de qa"):
		return "QA"
	case strings.Contains(lower, "agente de segurança") || strings.Contains(lower, "agente de security"):
		return "Segurança"
	case strings.Contains(lower, "frontend") || strings.Contains(lower, "front-end"):
		return "Frontend"
	case strings.Contains(lower, "backend") || strings.Contains(lower, "back-end"):
		return "Backend"
	case strings.Contains(lower, "banco de dados") || strings.Contains(lower, "database") || strings.Contains(lower, "migration"):
		return "Banco de Dados"
	case strings.Contains(lower, "testes") || strings.Contains(lower, "tests"):
		return "Testes"
	default:
		return "Agente"
	}
}
