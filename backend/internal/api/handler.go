package api

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wiltsou/quinoa/internal/db"
	"github.com/wiltsou/quinoa/internal/docker"
	"github.com/wiltsou/quinoa/templates"
)

type handler struct {
	db     *db.DB
	docker *docker.Client
}

// GET /
func (h *handler) indexPage(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.db.ListTasks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []*db.Task{}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.Index(tasks).Render(r.Context(), w)
}

// GET /tasks/{id}
func (h *handler) taskPage(w http.ResponseWriter, r *http.Request) {
	task, err := h.db.GetTask(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.TaskPage(task).Render(r.Context(), w)
}

// GET /api/tasks — returns HTMX fragment with task rows
func (h *handler) taskListFragment(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.db.ListTasks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []*db.Task{}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.TaskRows(tasks).Render(r.Context(), w)
}

// GET /api/tasks/{id}/status — returns HTMX fragment with status badge
func (h *handler) taskStatusFragment(w http.ResponseWriter, r *http.Request) {
	task, err := h.db.GetTask(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.StatusBadge(task.Status).Render(r.Context(), w)
}

// POST /tasks
func (h *handler) createTask(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	agentCommand := strings.TrimSpace(r.FormValue("agent_command"))
	repoURL := strings.TrimSpace(r.FormValue("repo_url"))
	repoPath := strings.TrimSpace(r.FormValue("repo_path"))
	repoBranch := strings.TrimSpace(r.FormValue("repo_branch"))

	if agentCommand == "" {
		httpError(w, "agent_command obrigatório", http.StatusBadRequest)
		return
	}
	if repoURL == "" && repoPath == "" {
		httpError(w, "repo_url ou repo_path obrigatório", http.StatusBadRequest)
		return
	}

	// Parse extra env vars (one per line, KEY=VALUE)
	var envExtra []string
	for _, line := range strings.Split(r.FormValue("env_extra"), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "=") {
			envExtra = append(envExtra, line)
		}
	}

	task := &db.Task{
		ID:           uuid.New().String(),
		RepoURL:      repoURL,
		RepoPath:     repoPath,
		RepoBranch:   repoBranch,
		AgentCommand: agentCommand,
		Status:       "pending",
	}
	if err := h.db.InsertTask(task); err != nil {
		httpError(w, "db: "+err.Error(), http.StatusInternalServerError)
		return
	}

	go h.runTask(task, envExtra)

	// HTMX redirect to terminal page
	w.Header().Set("HX-Redirect", "/tasks/"+task.ID)
	w.WriteHeader(http.StatusCreated)
}

// DELETE /tasks/{id}
func (h *handler) stopTask(w http.ResponseWriter, r *http.Request) {
	task, err := h.db.GetTask(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if task.ContainerID != "" {
		_ = h.docker.StopContainer(context.Background(), task.ContainerID)
	}
	_ = h.db.UpdateTaskStatus(task.ID, "stopped", task.ContainerID)
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusNoContent)
}

// runTask starts the container and monitors it until completion.
func (h *handler) runTask(task *db.Task, envExtra []string) {
	ctx := context.Background()

	containerID, err := h.docker.RunTask(ctx, docker.RunConfig{
		TaskID:       task.ID,
		RepoURL:      task.RepoURL,
		RepoPath:     task.RepoPath,
		RepoBranch:   task.RepoBranch,
		AgentCommand: task.AgentCommand,
		EnvExtra:     envExtra,
	})
	if err != nil {
		log.Printf("task %s: run error: %v", task.ID, err)
		_ = h.db.UpdateTaskStatus(task.ID, "error", "")
		return
	}

	_ = h.db.UpdateTaskStatus(task.ID, "running", containerID)
	log.Printf("task %s: container started: %s", task.ID, containerID[:12])

	// Monitor the container output for the [QUINOA:DONE] signal emitted by the agent.
	go h.watchForDoneSignal(task, containerID)

	exitCode, err := h.docker.WaitContainer(ctx, containerID)
	log.Printf("task %s: container exited exit=%d", task.ID, exitCode)

	// Only update status if watchForDoneSignal or moveStory haven't already set a
	// terminal state (idle / done / stopped).
	if cur, _ := h.db.GetTask(task.ID); cur != nil && cur.Status == "running" {
		finalStatus := "error"
		if err == nil && exitCode == 0 {
			finalStatus = "idle"
		}
		_ = h.db.UpdateTaskStatus(task.ID, finalStatus, containerID)
		if story, sErr := h.db.GetStoryByTaskID(task.ID); sErr == nil && story.KanbanStatus == "doing" {
			_ = h.db.UpdateStoryKanban(story.ID, "review", story.TaskID)
		}
	}

	// Remove container after 2h to free resources while still allowing late inspection
	time.AfterFunc(2*time.Hour, func() {
		_ = h.docker.RemoveContainer(context.Background(), containerID)
	})
}

type fsEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type fsResponse struct {
	Path   string    `json:"path"`
	Parent string    `json:"parent"`
	Home   string    `json:"home"`
	Dirs   []fsEntry `json:"dirs"`
}

// GET /api/fs?path=/some/dir — lists subdirectories at the given path.
func (h *handler) listDirectory(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = os.Getenv("HOME")
	}
	if path == "" {
		path = "/"
	}

	path = filepath.Clean(path)

	entries, err := os.ReadDir(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var dirs []fsEntry
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, fsEntry{
			Name: e.Name(),
			Path: filepath.Join(path, e.Name()),
		})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })

	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}

	home := os.Getenv("HOME")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fsResponse{
		Path:   path,
		Parent: parent,
		Home:   home,
		Dirs:   dirs,
	})
}

func splitStories(stories []*db.Story) (todo, doing, review, done []*db.Story) {
	for _, s := range stories {
		switch s.KanbanStatus {
		case "todo":
			todo = append(todo, s)
		case "doing":
			doing = append(doing, s)
		case "review":
			review = append(review, s)
		case "done":
			done = append(done, s)
		}
	}
	return
}

// GET /board
func (h *handler) boardPage(w http.ResponseWriter, r *http.Request) {
	stories, err := h.db.ListStories()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	todo, doing, review, done := splitStories(stories)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.Board(todo, doing, review, done).Render(r.Context(), w)
}

// GET /api/board — HTMX fragment with board columns
func (h *handler) boardFragment(w http.ResponseWriter, r *http.Request) {
	stories, err := h.db.ListStories()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	todo, doing, review, done := splitStories(stories)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.BoardCols(todo, doing, review, done).Render(r.Context(), w)
}

// POST /stories — create a new story in todo
func (h *handler) createStory(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		httpError(w, "title obrigatório", http.StatusBadRequest)
		return
	}
	s := &db.Story{
		ID:           uuid.New().String(),
		Title:        title,
		Description:  strings.TrimSpace(r.FormValue("description")),
		KanbanStatus: "todo",
	}
	if err := h.db.InsertStory(s); err != nil {
		httpError(w, "db: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", "/board")
	w.WriteHeader(http.StatusCreated)
}

// POST /stories/start — assign agent to story and move to doing
func (h *handler) startStory(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	storyID := strings.TrimSpace(r.FormValue("story_id"))
	story, err := h.db.GetStory(storyID)
	if err != nil {
		http.Error(w, "story not found", http.StatusNotFound)
		return
	}

	agentCommand := strings.TrimSpace(r.FormValue("agent_command"))
	repoURL := strings.TrimSpace(r.FormValue("repo_url"))
	repoPath := strings.TrimSpace(r.FormValue("repo_path"))
	repoBranch := strings.TrimSpace(r.FormValue("repo_branch"))

	if agentCommand == "" {
		httpError(w, "agent_command obrigatório", http.StatusBadRequest)
		return
	}
	if repoURL == "" && repoPath == "" {
		httpError(w, "repo_url ou repo_path obrigatório", http.StatusBadRequest)
		return
	}

	var envExtra []string
	for _, line := range strings.Split(r.FormValue("env_extra"), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "=") {
			envExtra = append(envExtra, line)
		}
	}

	// Append story title + description as the initial prompt for the agent.
	prompt := story.Title
	if story.Description != "" {
		prompt += "\n\n" + story.Description
	}
	agentCommand += " " + shellQuote(prompt)

	task := &db.Task{
		ID:           uuid.New().String(),
		RepoURL:      repoURL,
		RepoPath:     repoPath,
		RepoBranch:   repoBranch,
		AgentCommand: agentCommand,
		Status:       "pending",
	}
	if err := h.db.InsertTask(task); err != nil {
		httpError(w, "db: "+err.Error(), http.StatusInternalServerError)
		return
	}
	go h.runTask(task, envExtra)

	if err := h.db.UpdateStoryKanban(story.ID, "doing", task.ID); err != nil {
		httpError(w, "db: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/board")
	w.WriteHeader(http.StatusCreated)
}

// POST /stories/{id}/move — move story to todo or done
func (h *handler) moveStory(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	story, err := h.db.GetStory(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	to := r.FormValue("to")
	if to != "todo" && to != "doing" && to != "review" && to != "done" {
		httpError(w, "invalid status", http.StatusBadRequest)
		return
	}
	taskID := story.TaskID
	switch to {
	case "todo":
		taskID = "" // unlink task; stop container if still running
		if story.TaskID != "" {
			if task, err := h.db.GetTask(story.TaskID); err == nil && task.ContainerID != "" {
				_ = h.docker.StopContainer(context.Background(), task.ContainerID)
				_ = h.db.UpdateTaskStatus(task.ID, "stopped", task.ContainerID)
			}
		}
	case "doing":
		// Continuation: story sent back from review — restore task to running
		if story.TaskID != "" {
			if task, err := h.db.GetTask(story.TaskID); err == nil && task.ContainerID != "" {
				_ = h.db.UpdateTaskStatus(task.ID, "running", task.ContainerID)
			}
		}
	case "done":
		if story.TaskID != "" {
			if task, err := h.db.GetTask(story.TaskID); err == nil && task.ContainerID != "" {
				_ = h.docker.StopContainer(context.Background(), task.ContainerID)
				_ = h.db.UpdateTaskStatus(task.ID, "done", task.ContainerID)
			}
		}
	}
	if err := h.db.UpdateStoryKanban(story.ID, to, taskID); err != nil {
		httpError(w, "db: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", "/board")
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /stories/{id}
func (h *handler) deleteStory(w http.ResponseWriter, r *http.Request) {
	if err := h.db.DeleteStory(r.PathValue("id")); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("HX-Redirect", "/board")
	w.WriteHeader(http.StatusNoContent)
}

// watchForDoneSignal streams the container logs looking for the [QUINOA:DONE] marker
// that the agent emits when it finishes a task. On each detection it sets the task
// status to idle and auto-moves the associated story to the Review column — but only
// when the story is still in "doing", so multiple corrections in the same session work.
func (h *handler) watchForDoneSignal(task *db.Task, containerID string) {
	ctx := context.Background()
	stream, err := h.docker.StreamLogs(ctx, containerID)
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
		story, err := h.db.GetStoryByTaskID(task.ID)
		if err != nil || story.KanbanStatus != "doing" {
			continue
		}
		_ = h.db.UpdateTaskStatus(task.ID, "idle", containerID)
		_ = h.db.UpdateStoryKanban(story.ID, "review", story.TaskID)
		log.Printf("task %s: agent signalled completion → review", task.ID)
	}
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
// Safe to embed in a bash -c string.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func httpError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

