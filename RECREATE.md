# Quinoa — Prompt de Recriação

Cole este prompt em uma nova sessão do Claude Code para recriar o projeto do zero.

---

Crie o projeto quinoa no diretório atual. Quinoa é uma plataforma de orquestração de agentes IA (Claude Code, Aider etc.) rodando em containers Docker isolados, com UI web em Go + HTMX + Templ + xterm.js.

Crie EXATAMENTE os arquivos abaixo com o conteúdo fornecido.

## Estrutura de diretórios

```
quinoa/
├── backend/
│   ├── main.go
│   ├── go.mod
│   ├── Dockerfile
│   └── internal/
│       ├── api/
│       │   ├── router.go
│       │   ├── handler.go
│       │   └── ws.go
│       ├── db/
│       │   └── db.go
│       ├── docker/
│       │   └── client.go
│       └── browser/
│           └── browser.go
│   └── templates/
│       ├── helpers.go
│       ├── layout.templ
│       ├── index.templ
│       ├── task.templ
│       └── board.templ
├── sandbox/
│   ├── Dockerfile
│   └── runner.sh
├── docker-compose.yml
└── Makefile
```

## FILE: backend/go.mod

```
module github.com/wiltsou/quinoa

go 1.25.0

require (
	github.com/docker/docker v27.3.1+incompatible
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	modernc.org/sqlite v1.33.1
)

require (
	github.com/Microsoft/go-winio v0.4.14 // indirect
	github.com/a-h/templ v0.2.793 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/morikuni/aec v1.1.0 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.24.0 // indirect
	gotest.tools/v3 v3.5.2 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)
```

## FILE: backend/main.go

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/wiltsou/quinoa/internal/api"
	"github.com/wiltsou/quinoa/internal/browser"
	"github.com/wiltsou/quinoa/internal/db"
	"github.com/wiltsou/quinoa/internal/docker"
)

func main() {
	dataDir := dataDirectory()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	database, err := db.New(filepath.Join(dataDir, "quinoa.db"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Fatalf("docker: %v", err)
	}

	router := api.NewRouter(database, dockerClient)

	port := os.Getenv("PORT")
	if port == "" {
		port = freePort()
	}
	addr := "127.0.0.1:" + port

	srv := &http.Server{Addr: addr, Handler: router}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	url := "http://" + addr
	log.Printf("quinoa at %s", url)

	if os.Getenv("QUINOA_HEADLESS") != "1" {
		time.Sleep(150 * time.Millisecond)
		if err := browser.Open(url); err != nil {
			log.Printf("browser: %v — abra %s manualmente", err, url)
		}
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("quinoa encerrando...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func freePort() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "8080"
	}
	defer l.Close()
	return fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
}

func dataDirectory() string {
	if dir := os.Getenv("DATA_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".quinoa")
}
```

## FILE: backend/internal/api/router.go

```go
package api

import (
	"net/http"

	"github.com/wiltsou/quinoa/internal/db"
	"github.com/wiltsou/quinoa/internal/docker"
)

func NewRouter(database *db.DB, dockerClient *docker.Client) http.Handler {
	h := &handler{db: database, docker: dockerClient}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", h.indexPage)
	mux.HandleFunc("GET /tasks/{id}", h.taskPage)
	mux.HandleFunc("GET /board", h.boardPage)

	mux.HandleFunc("POST /tasks", h.createTask)
	mux.HandleFunc("DELETE /tasks/{id}", h.stopTask)

	mux.HandleFunc("POST /stories", h.createStory)
	mux.HandleFunc("POST /stories/start", h.startStory)
	mux.HandleFunc("POST /stories/{id}/move", h.moveStory)
	mux.HandleFunc("DELETE /stories/{id}", h.deleteStory)

	mux.HandleFunc("GET /api/tasks", h.taskListFragment)
	mux.HandleFunc("GET /api/tasks/{id}/status", h.taskStatusFragment)
	mux.HandleFunc("GET /api/board", h.boardFragment)
	mux.HandleFunc("GET /api/fs", h.listDirectory)

	mux.HandleFunc("GET /ws/tasks/{id}/terminal", h.terminalWS)

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return corsMiddleware(mux)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,HX-Request")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

## FILE: backend/internal/api/handler.go

```go
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

func (h *handler) taskPage(w http.ResponseWriter, r *http.Request) {
	task, err := h.db.GetTask(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.TaskPage(task).Render(r.Context(), w)
}

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

func (h *handler) taskStatusFragment(w http.ResponseWriter, r *http.Request) {
	task, err := h.db.GetTask(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.StatusBadge(task.Status).Render(r.Context(), w)
}

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

	w.Header().Set("HX-Redirect", "/tasks/"+task.ID)
	w.WriteHeader(http.StatusCreated)
}

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

	go h.watchForDoneSignal(task, containerID)

	exitCode, err := h.docker.WaitContainer(ctx, containerID)
	log.Printf("task %s: container exited exit=%d", task.ID, exitCode)

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
		taskID = ""
		if story.TaskID != "" {
			if task, err := h.db.GetTask(story.TaskID); err == nil && task.ContainerID != "" {
				_ = h.docker.StopContainer(context.Background(), task.ContainerID)
				_ = h.db.UpdateTaskStatus(task.ID, "stopped", task.ContainerID)
			}
		}
	case "doing":
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

func (h *handler) deleteStory(w http.ResponseWriter, r *http.Request) {
	if err := h.db.DeleteStory(r.PathValue("id")); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("HX-Redirect", "/board")
	w.WriteHeader(http.StatusNoContent)
}

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

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func httpError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
```

## FILE: backend/internal/api/ws.go

```go
package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type resizeMsg struct {
	Type string `json:"type"`
	Cols uint   `json:"cols"`
	Rows uint   `json:"rows"`
}

func (h *handler) terminalWS(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")

	task, err := h.db.GetTask(taskID)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.ContainerID == "" {
		http.Error(w, "container not started yet", http.StatusServiceUnavailable)
		return
	}

	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer wsConn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if logs, err := h.docker.GetLogs(ctx, task.ContainerID); err == nil {
		buf := make([]byte, 4096)
		for {
			n, err := logs.Read(buf)
			if n > 0 {
				wsConn.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
			if err != nil {
				break
			}
		}
		logs.Close()
	}

	attach, err := h.docker.AttachTerminal(ctx, task.ContainerID)
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte("\r\n[quinoa] erro ao conectar ao container: "+err.Error()+"\r\n"))
		return
	}
	defer attach.Close()

	go func() {
		defer cancel()
		buf := make([]byte, 4096)
		for {
			n, err := attach.Reader.Read(buf)
			if n > 0 {
				if werr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("task %s: pty read: %v", taskID, err)
				}
				wsConn.WriteMessage(websocket.TextMessage, []byte("\r\n[quinoa] sessão encerrada\r\n"))
				return
			}
		}
	}()

	for {
		msgType, msg, err := wsConn.ReadMessage()
		if err != nil {
			return
		}
		switch msgType {
		case websocket.BinaryMessage:
			if _, err := attach.Conn.Write(msg); err != nil {
				return
			}
		case websocket.TextMessage:
			var ev resizeMsg
			if json.Unmarshal(msg, &ev) == nil && ev.Type == "resize" && ev.Cols > 0 && ev.Rows > 0 {
				if err := h.docker.ResizeTerminal(ctx, task.ContainerID, ev.Rows, ev.Cols); err != nil {
					log.Printf("task %s: resize: %v", taskID, err)
				}
			}
		}
	}
}
```

## FILE: backend/internal/db/db.go

```go
package db

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct{ *sql.DB }

type Task struct {
	ID           string    `json:"id"`
	RepoURL      string    `json:"repo_url"`
	RepoPath     string    `json:"repo_path"`
	RepoBranch   string    `json:"repo_branch"`
	AgentCommand string    `json:"agent_command"`
	ContainerID  string    `json:"container_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Story struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	KanbanStatus string    `json:"kanban_status"`
	TaskID       string    `json:"task_id"`
	TaskStatus   string    `json:"task_status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func New(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	d := &DB{sqldb}
	return d, d.migrate()
}

func (d *DB) migrate() error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id            TEXT PRIMARY KEY,
			repo_url      TEXT NOT NULL DEFAULT '',
			repo_path     TEXT NOT NULL DEFAULT '',
			repo_branch   TEXT NOT NULL DEFAULT '',
			agent_command TEXT NOT NULL,
			container_id  TEXT NOT NULL DEFAULT '',
			status        TEXT NOT NULL DEFAULT 'pending',
			created_at    DATETIME NOT NULL,
			updated_at    DATETIME NOT NULL
		)
	`)
	if err != nil {
		return err
	}
	_, err = d.Exec(`
		CREATE TABLE IF NOT EXISTS stories (
			id            TEXT PRIMARY KEY,
			title         TEXT NOT NULL,
			description   TEXT NOT NULL DEFAULT '',
			kanban_status TEXT NOT NULL DEFAULT 'todo',
			task_id       TEXT NOT NULL DEFAULT '',
			created_at    DATETIME NOT NULL,
			updated_at    DATETIME NOT NULL
		)
	`)
	return err
}

func (d *DB) InsertTask(t *Task) error {
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := d.Exec(`
		INSERT INTO tasks (id, repo_url, repo_path, repo_branch, agent_command, container_id, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.RepoURL, t.RepoPath, t.RepoBranch, t.AgentCommand, t.ContainerID, t.Status, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (d *DB) UpdateTaskStatus(id, status, containerID string) error {
	_, err := d.Exec(`UPDATE tasks SET status=?, container_id=?, updated_at=? WHERE id=?`,
		status, containerID, time.Now().UTC(), id)
	return err
}

func (d *DB) GetTask(id string) (*Task, error) {
	row := d.QueryRow(`
		SELECT id, repo_url, repo_path, repo_branch, agent_command, container_id, status, created_at, updated_at
		FROM tasks WHERE id=?`, id)
	return scanTask(row)
}

func (d *DB) ListTasks() ([]*Task, error) {
	rows, err := d.Query(`
		SELECT id, repo_url, repo_path, repo_branch, agent_command, container_id, status, created_at, updated_at
		FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (*Task, error) {
	t := &Task{}
	err := s.Scan(&t.ID, &t.RepoURL, &t.RepoPath, &t.RepoBranch, &t.AgentCommand, &t.ContainerID, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func (d *DB) InsertStory(s *Story) error {
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now
	_, err := d.Exec(`
		INSERT INTO stories (id, title, description, kanban_status, task_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Title, s.Description, s.KanbanStatus, s.TaskID, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (d *DB) ListStories() ([]*Story, error) {
	rows, err := d.Query(`
		SELECT s.id, s.title, s.description, s.kanban_status, s.task_id,
		       COALESCE(t.status, '') AS task_status,
		       s.created_at, s.updated_at
		FROM stories s
		LEFT JOIN tasks t ON s.task_id = t.id AND s.task_id != ''
		ORDER BY s.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stories []*Story
	for rows.Next() {
		s := &Story{}
		if err := rows.Scan(&s.ID, &s.Title, &s.Description, &s.KanbanStatus, &s.TaskID,
			&s.TaskStatus, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		stories = append(stories, s)
	}
	return stories, rows.Err()
}

func (d *DB) GetStory(id string) (*Story, error) {
	row := d.QueryRow(`
		SELECT s.id, s.title, s.description, s.kanban_status, s.task_id,
		       COALESCE(t.status, '') AS task_status,
		       s.created_at, s.updated_at
		FROM stories s
		LEFT JOIN tasks t ON s.task_id = t.id AND s.task_id != ''
		WHERE s.id = ?`, id)
	s := &Story{}
	err := row.Scan(&s.ID, &s.Title, &s.Description, &s.KanbanStatus, &s.TaskID,
		&s.TaskStatus, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func (d *DB) UpdateStoryKanban(id, kanbanStatus, taskID string) error {
	_, err := d.Exec(`UPDATE stories SET kanban_status=?, task_id=?, updated_at=? WHERE id=?`,
		kanbanStatus, taskID, time.Now().UTC(), id)
	return err
}

func (d *DB) DeleteStory(id string) error {
	_, err := d.Exec(`DELETE FROM stories WHERE id=?`, id)
	return err
}

func (d *DB) GetStoryByTaskID(taskID string) (*Story, error) {
	row := d.QueryRow(`
		SELECT s.id, s.title, s.description, s.kanban_status, s.task_id,
		       COALESCE(t.status, '') AS task_status,
		       s.created_at, s.updated_at
		FROM stories s
		LEFT JOIN tasks t ON s.task_id = t.id AND s.task_id != ''
		WHERE s.task_id = ?`, taskID)
	s := &Story{}
	err := row.Scan(&s.ID, &s.Title, &s.Description, &s.KanbanStatus, &s.TaskID,
		&s.TaskStatus, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}
```

## FILE: backend/internal/docker/client.go

```go
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
	RepoURL      string
	RepoPath     string
	RepoBranch   string
	AgentCommand string
	EnvExtra     []string
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

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		env = append(env, "ANTHROPIC_API_KEY="+key)
	}

	resp, err := c.cli.ContainerCreate(ctx,
		&container.Config{
			Image:        sandboxImage,
			Env:          env,
			Tty:          true,
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			OpenStdin:    true,
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

func (c *Client) AttachTerminal(ctx context.Context, containerID string) (dtypes.HijackedResponse, error) {
	return c.cli.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
}

func (c *Client) GetLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
	})
}

func (c *Client) StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
}

func (c *Client) ResizeTerminal(ctx context.Context, containerID string, rows, cols uint) error {
	return c.cli.ContainerResize(ctx, containerID, container.ResizeOptions{
		Height: rows,
		Width:  cols,
	})
}

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
```

## FILE: backend/internal/browser/browser.go

```go
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func Open(url string) error {
	switch runtime.GOOS {
	case "linux":
		return openLinux(url)
	case "darwin":
		return openDarwin(url)
	case "windows":
		return openWindows(url)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

var chromiumCandidates = []string{
	"google-chrome", "google-chrome-stable", "chromium",
	"chromium-browser", "microsoft-edge", "microsoft-edge-stable", "brave-browser",
}

var appFlags = []string{
	"--no-first-run", "--no-default-browser-check", "--window-size=1400,900",
}

func openLinux(url string) error {
	for _, bin := range chromiumCandidates {
		if path, err := exec.LookPath(bin); err == nil {
			args := append([]string{"--app=" + url}, appFlags...)
			return exec.Command(path, args...).Start()
		}
	}
	return exec.Command("xdg-open", url).Start()
}

func openDarwin(url string) error {
	appPaths := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
	}
	for _, p := range appPaths {
		if _, err := os.Stat(p); err == nil {
			args := append([]string{"--app=" + url}, appFlags...)
			return exec.Command(p, args...).Start()
		}
	}
	return exec.Command("open", url).Start()
}

func openWindows(url string) error {
	for _, bin := range []string{"msedge", "chrome", "chromium"} {
		if path, err := exec.LookPath(bin); err == nil {
			args := append([]string{"--app=" + url}, appFlags...)
			return exec.Command(path, args...).Start()
		}
	}
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
```

## FILE: backend/templates/helpers.go

```go
package templates

import (
	"fmt"
	"strings"

	"github.com/wiltsou/quinoa/internal/db"
)

func repoLabel(t *db.Task) string {
	if t.RepoURL != "" {
		parts := strings.Split(strings.TrimSuffix(t.RepoURL, ".git"), "/")
		if len(parts) >= 2 {
			return fmt.Sprintf("%s/%s", parts[len(parts)-2], parts[len(parts)-1])
		}
		return t.RepoURL
	}
	if t.RepoPath != "" {
		parts := strings.Split(strings.TrimRight(t.RepoPath, "/"), "/")
		return parts[len(parts)-1]
	}
	return t.ID[:8]
}
```

## FILE: backend/templates/layout.templ

```templ
package templates

templ Nav(active string) {
	<div class="topbar-sep"></div>
	<nav class="topbar-nav">
		<a href="/" class={ "nav-link", templ.KV("active", active == "agents") }>Agentes</a>
		<a href="/board" class={ "nav-link", templ.KV("active", active == "board") }>Board</a>
	</nav>
}

templ Layout(title string) {
	<!DOCTYPE html>
	<html lang="pt-BR">
		<head>
			<meta charset="UTF-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1.0"/>
			<title>{ title } — quinoa</title>
			<script src="https://unpkg.com/htmx.org@2.0.3/dist/htmx.min.js"></script>
			<style>
				*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
				:root {
					--bg: #0d1117; --bg2: #161b22; --bg3: #21262d; --border: #30363d;
					--text: #e6edf3; --muted: #8b949e; --accent: #58a6ff;
					--green: #3fb950; --red: #f85149; --yellow: #d29922; --orange: #e3b341;
					--radius: 6px;
					--font-mono: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'Consolas', monospace;
					--font-ui: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
				}
				html, body { height: 100%; background: var(--bg); color: var(--text); font-family: var(--font-ui); font-size: 14px; line-height: 1.5; }
				a { color: var(--accent); text-decoration: none; }
				a:hover { text-decoration: underline; }
				button, .btn {
					display: inline-flex; align-items: center; gap: 6px;
					padding: 6px 14px; border-radius: var(--radius); border: 1px solid var(--border);
					background: var(--bg3); color: var(--text); font-size: 13px; cursor: pointer;
					transition: background 0.15s, border-color 0.15s;
				}
				button:hover, .btn:hover { background: var(--bg2); border-color: var(--accent); }
				button.primary { background: var(--accent); color: #0d1117; border-color: var(--accent); font-weight: 600; }
				button.primary:hover { background: #79c0ff; border-color: #79c0ff; }
				button.danger { background: transparent; color: var(--red); border-color: var(--red); }
				button.danger:hover { background: rgba(248,81,73,.1); }
				input, textarea, select {
					width: 100%; padding: 6px 10px; border-radius: var(--radius);
					border: 1px solid var(--border); background: var(--bg3); color: var(--text);
					font-family: var(--font-ui); font-size: 13px; outline: none;
					transition: border-color 0.15s;
				}
				input:focus, textarea:focus, select:focus { border-color: var(--accent); }
				textarea { font-family: var(--font-mono); resize: vertical; }
				label { display: block; margin-bottom: 4px; font-size: 12px; color: var(--muted); font-weight: 500; letter-spacing: .3px; text-transform: uppercase; }
				.field { margin-bottom: 16px; }
				.badge {
					display: inline-flex; align-items: center; gap: 5px;
					padding: 2px 8px; border-radius: 20px; font-size: 11px; font-weight: 600;
					letter-spacing: .3px; text-transform: uppercase;
				}
				.badge::before { content: ''; width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
				.badge.running  { color: var(--green);  background: rgba(63,185,80,.12);  }
				.badge.pending  { color: var(--yellow); background: rgba(210,153,34,.12); }
				.badge.idle     { color: var(--accent); background: rgba(88,166,255,.12); }
				.badge.done     { color: var(--muted);  background: rgba(139,148,158,.12);}
				.badge.error    { color: var(--red);    background: rgba(248,81,73,.12);  }
				.badge.stopped  { color: var(--orange); background: rgba(227,179,65,.12); }
				.topbar {
					display: flex; align-items: center; gap: 12px;
					padding: 12px 20px; border-bottom: 1px solid var(--border);
					background: var(--bg2); height: 52px;
				}
				.topbar .logo { font-family: var(--font-mono); font-weight: 700; font-size: 16px; color: var(--text); letter-spacing: -0.5px; text-decoration: none; }
				.topbar .sep  { color: var(--border); }
				.topbar .spacer { flex: 1; }
				.topbar-sep { width: 1px; height: 20px; background: var(--border); flex-shrink: 0; }
				.topbar-nav { display: flex; gap: 2px; }
				.nav-link { padding: 4px 12px; border-radius: var(--radius); font-size: 13px; color: var(--muted); text-decoration: none; transition: background .15s, color .15s; }
				.nav-link:hover { color: var(--text); background: var(--bg3); text-decoration: none; }
				.nav-link.active { color: var(--text); background: var(--bg3); }
			</style>
		</head>
		<body>
			{ children... }
			<script>
			(function() {
				if ('Notification' in window && Notification.permission === 'default') {
					Notification.requestPermission();
				}
				window.quinoaNotify = function(title, body, url) {
					if (!('Notification' in window) || Notification.permission !== 'granted') return;
					var n = new Notification(title, { body: body });
					if (url) n.onclick = function() { window.focus(); location.href = url; };
					setTimeout(function() { n.close(); }, 10000);
				};
			})();
			</script>
		</body>
	</html>
}
```

## FILE: backend/templates/index.templ

```templ
package templates

import (
	"fmt"
	"time"

	"github.com/wiltsou/quinoa/internal/db"
)

templ Index(tasks []*db.Task) {
	@Layout("Tarefas") {
		<style>
			.page { max-width: 860px; margin: 0 auto; padding: 32px 20px; }
			.page-header { display: flex; align-items: center; margin-bottom: 28px; }
			.page-header h1 { font-family: var(--font-mono); font-size: 22px; font-weight: 700; letter-spacing: -0.5px; }
			.page-header .spacer { flex: 1; }
			.form-card { background: var(--bg2); border: 1px solid var(--border); border-radius: var(--radius); padding: 20px; margin-bottom: 28px; display: none; }
			.form-card.open { display: block; }
			.form-card h2 { font-size: 14px; font-weight: 600; margin-bottom: 16px; color: var(--muted); text-transform: uppercase; letter-spacing: .5px; }
			.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
			.path-picker { display: flex; align-items: center; gap: 8px; }
			.path-picker-label { font-size: 12px; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
			.dir-modal-overlay { display: none; position: fixed; inset: 0; z-index: 200; background: rgba(0,0,0,.6); align-items: center; justify-content: center; }
			.dir-modal-overlay.open { display: flex; }
			.dir-modal { background: var(--bg2); border: 1px solid var(--border); border-radius: var(--radius); width: 520px; max-width: 95vw; display: flex; flex-direction: column; max-height: 70vh; }
			.dir-modal-header { padding: 14px 16px; border-bottom: 1px solid var(--border); display: flex; align-items: center; gap: 8px; }
			.dir-modal-header .dir-path { font-family: var(--font-mono); font-size: 12px; color: var(--muted); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
			.dir-modal-list { flex: 1; overflow-y: auto; padding: 8px 0; }
			.dir-entry { padding: 8px 16px; cursor: pointer; font-family: var(--font-mono); font-size: 13px; display: flex; align-items: center; gap: 8px; }
			.dir-entry:hover { background: var(--bg3, #21262d); }
			.dir-entry.up { color: var(--muted); }
			.dir-entry::before { content: '📁'; font-size: 14px; }
			.dir-entry.up::before { content: '↑'; font-style: normal; font-size: 14px; }
			.dir-empty { padding: 24px 16px; color: var(--muted); font-size: 13px; text-align: center; }
			.dir-modal-footer { padding: 12px 16px; border-top: 1px solid var(--border); display: flex; justify-content: flex-end; gap: 8px; }
			.task-table { width: 100%; border-collapse: collapse; }
			.task-table th { text-align: left; padding: 8px 12px; font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: .3px; border-bottom: 1px solid var(--border); }
			.task-table td { padding: 12px; border-bottom: 1px solid var(--border); vertical-align: middle; }
			.task-table tr:last-child td { border-bottom: none; }
			.task-table tr:hover td { background: var(--bg2); }
			.task-table .mono { font-family: var(--font-mono); font-size: 12px; }
			.empty-state { text-align: center; padding: 60px 20px; color: var(--muted); }
			.empty-state p { margin-top: 8px; font-size: 13px; }
		</style>
		<div class="topbar">
			<a href="/" class="logo">quinoa</a>
			@Nav("agents")
		</div>
		<div class="page">
			<div class="page-header">
				<h1>Agentes</h1>
				<div class="spacer"></div>
				<button class="primary" onclick="document.getElementById('new-task-form').classList.toggle('open')">+ Nova tarefa</button>
			</div>
			<div id="new-task-form" class="form-card">
				<h2>Nova Tarefa</h2>
				<form hx-post="/tasks" hx-target="body">
					<div class="form-row">
						<div class="field">
							<label>URL do repositório (git clone)</label>
							<input type="text" name="repo_url" placeholder="https://github.com/user/repo.git"/>
						</div>
						<div class="field">
							<label>Pasta local</label>
							<div class="path-picker">
								<input type="hidden" id="repo-path-value" name="repo_path" value=""/>
								<span id="repo-path-label" class="path-picker-label" style="color:var(--muted)">Nenhuma pasta selecionada</span>
								<button type="button" class="btn" style="white-space:nowrap;flex-shrink:0" onclick="openDirModal()">Selecionar pasta</button>
							</div>
						</div>
					</div>
					<div class="form-row">
						<div class="field">
							<label>Branch (opcional)</label>
							<input type="text" name="repo_branch" placeholder="main"/>
						</div>
						<div class="field">
							<label>Agente</label>
							<select id="agent-preset" onchange="onAgentPresetChange(this)">
								<option value="claude --dangerously-skip-permissions" selected>Claude Code</option>
								<option value="aider --yes-always">Aider</option>
								<option value="bash">Bash (manual)</option>
								<option value="">Personalizado…</option>
							</select>
							<input type="hidden" id="agent-command-value" name="agent_command" value="claude --dangerously-skip-permissions"/>
							<input type="text" id="agent-command-custom" placeholder="ex: meu-agente --flag" style="display:none;margin-top:6px"/>
						</div>
					</div>
					<div class="field">
						<label>Variáveis de ambiente extras (uma por linha: KEY=VALUE)</label>
						<textarea name="env_extra" rows="3" placeholder="ANTHROPIC_API_KEY=sk-ant-...&#10;CUSTOM_VAR=value"></textarea>
					</div>
					<div style="display:flex;gap:8px;justify-content:flex-end">
						<button type="button" onclick="document.getElementById('new-task-form').classList.remove('open')">Cancelar</button>
						<button type="submit" class="primary">Iniciar</button>
					</div>
				</form>
			</div>
			<div id="dir-modal-overlay" class="dir-modal-overlay" onclick="onOverlayClick(event)">
				<div class="dir-modal">
					<div class="dir-modal-header">
						<button type="button" class="btn" onclick="dirUp()" id="dir-up-btn" title="Pasta acima">↑</button>
						<span class="dir-path" id="dir-current-path"></span>
					</div>
					<div class="dir-modal-list" id="dir-list"></div>
					<div class="dir-modal-footer">
						<button type="button" onclick="closeDirModal()">Cancelar</button>
						<button type="button" class="primary" onclick="confirmDir()">Selecionar esta pasta</button>
					</div>
				</div>
			</div>
			<div id="task-list" hx-get="/api/tasks" hx-trigger="every 3s" hx-swap="innerHTML">
				@TaskRows(tasks)
			</div>
		</div>
		<script>
		function onAgentPresetChange(sel) {
			const hidden = document.getElementById('agent-command-value');
			const custom = document.getElementById('agent-command-custom');
			if (sel.value !== '') { hidden.value = sel.value; custom.style.display = 'none'; }
			else { hidden.value = ''; custom.style.display = ''; custom.focus(); custom.oninput = function() { hidden.value = custom.value; }; }
		}
		let dirCurrentPath = '', dirParentPath = '';
		function openDirModal() { navigateTo(document.getElementById('repo-path-value').value || null); document.getElementById('dir-modal-overlay').classList.add('open'); }
		function closeDirModal() { document.getElementById('dir-modal-overlay').classList.remove('open'); }
		function onOverlayClick(e) { if (e.target === document.getElementById('dir-modal-overlay')) closeDirModal(); }
		function dirUp() { if (dirParentPath) navigateTo(dirParentPath); }
		function navigateTo(path) {
			fetch('/api/fs' + (path ? '?path=' + encodeURIComponent(path) : '')).then(r => r.json()).then(renderDir).catch(() => { document.getElementById('dir-list').innerHTML = '<div class="dir-empty">Erro ao listar pasta</div>'; });
		}
		function renderDir(data) {
			dirCurrentPath = data.path; dirParentPath = data.parent || '';
			document.getElementById('dir-current-path').textContent = data.path;
			document.getElementById('dir-up-btn').disabled = !dirParentPath;
			const list = document.getElementById('dir-list');
			list.innerHTML = '';
			if (!data.dirs || data.dirs.length === 0) { list.innerHTML = '<div class="dir-empty">Nenhuma subpasta encontrada</div>'; return; }
			data.dirs.forEach(entry => { const el = document.createElement('div'); el.className = 'dir-entry'; el.textContent = entry.name; el.onclick = () => navigateTo(entry.path); list.appendChild(el); });
		}
		function confirmDir() {
			document.getElementById('repo-path-value').value = dirCurrentPath;
			document.getElementById('repo-path-label').textContent = dirCurrentPath;
			document.getElementById('repo-path-label').style.color = 'var(--text)';
			closeDirModal();
		}
		</script>
	}
}

templ TaskRows(tasks []*db.Task) {
	if len(tasks) == 0 {
		<div class="empty-state">
			<div style="font-size:32px">⬡</div>
			<p>Nenhuma tarefa ainda. Crie uma para começar.</p>
		</div>
	} else {
		<table class="task-table">
			<thead>
				<tr>
					<th>Status</th><th>Repositório</th><th>Agente</th><th>Criado</th><th></th>
				</tr>
			</thead>
			<tbody>
				for _, t := range tasks {
					@taskRow(t)
				}
			</tbody>
		</table>
	}
}

templ taskRow(t *db.Task) {
	<tr>
		<td>@StatusBadge(t.Status)</td>
		<td class="mono">{ repoLabelTempl(t) }</td>
		<td class="mono" style="color:var(--muted);max-width:260px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{ t.AgentCommand }</td>
		<td style="color:var(--muted);font-size:12px">{ humanTime(t.CreatedAt) }</td>
		<td style="text-align:right;white-space:nowrap">
			<a href={ templ.SafeURL("/tasks/" + t.ID) } class="btn" style="font-size:12px">Abrir terminal →</a>
		</td>
	</tr>
}

templ StatusBadge(status string) {
	<span class={ "badge " + status }>{ status }</span>
}

func repoLabelTempl(t *db.Task) string { return repoLabel(t) }

func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "agora"
	case d < time.Hour:
		return fmt.Sprintf("há %dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("há %dh", int(d.Hours()))
	default:
		return t.Format("02/01 15:04")
	}
}
```

## FILE: backend/templates/task.templ

```templ
package templates

import "github.com/wiltsou/quinoa/internal/db"

templ TaskPage(t *db.Task) {
	@Layout(repoLabelTempl(t)) {
		<style>
			.terminal-layout { display: flex; flex-direction: column; height: 100vh; overflow: hidden; }
			.terminal-topbar { display: flex; align-items: center; gap: 12px; flex-shrink: 0; padding: 0 20px; border-bottom: 1px solid var(--border); background: var(--bg2); height: 52px; }
			.terminal-topbar .logo { font-family: var(--font-mono); font-weight: 700; font-size: 16px; }
			.terminal-topbar .sep  { color: var(--border); }
			.terminal-topbar .repo { font-family: var(--font-mono); font-size: 13px; }
			.terminal-topbar .spacer { flex: 1; }
			.terminal-topbar .meta { font-size: 12px; color: var(--muted); }
			#terminal-wrap { flex: 1; overflow: hidden; background: #0d1117; padding: 4px; }
			#terminal-wrap .xterm { height: 100%; }
			#terminal-wrap .xterm-viewport { overflow-y: auto !important; }
			.conn-banner { position: fixed; bottom: 16px; right: 16px; z-index: 100; background: var(--bg3); border: 1px solid var(--border); border-radius: var(--radius); padding: 10px 16px; font-size: 12px; color: var(--muted); display: none; gap: 8px; align-items: center; }
			.conn-banner.visible { display: flex; }
			.conn-banner.error { border-color: var(--red); color: var(--red); }
		</style>
		<div class="terminal-layout">
			<div class="terminal-topbar">
				<a href="/" class="logo" style="color:var(--text)">quinoa</a>
				<span class="sep">›</span>
				<span class="repo">{ repoLabelTempl(t) }</span>
				<div id="status-badge" hx-get={ "/api/tasks/" + t.ID + "/status" } hx-trigger={ statusTrigger(t.Status) } hx-swap="outerHTML">
					@StatusBadge(t.Status)
				</div>
				<div class="spacer"></div>
				<span class="meta">{ t.AgentCommand }</span>
				if t.Status == "running" || t.Status == "pending" {
					<button class="danger" hx-delete={ "/tasks/" + t.ID } hx-confirm="Parar este agente?">Parar</button>
				}
			</div>
			<div id="terminal-wrap" data-ws-url={ "/ws/tasks/" + t.ID + "/terminal" } data-status={ t.Status }></div>
		</div>
		<div id="conn-banner" class="conn-banner"></div>
		<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/>
		<script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js"></script>
		<script src="https://cdn.jsdelivr.net/npm/xterm-addon-fit@0.8.0/lib/xterm-addon-fit.js"></script>
		<script src="https://cdn.jsdelivr.net/npm/xterm-addon-web-links@0.9.0/lib/xterm-addon-web-links.js"></script>
		<script>
		(function () {
			const wrap = document.getElementById('terminal-wrap');
			const banner = document.getElementById('conn-banner');
			const wsUrl = wrap.dataset.wsUrl;
			const status = wrap.dataset.status;
			let taskStatus = status;
			const repoLabel = (document.querySelector('.terminal-topbar .repo') || {}).textContent || '';
			let idleTimer;
			const IDLE_MS = 3 * 60 * 1000;
			function resetIdle() {
				clearTimeout(idleTimer);
				if (taskStatus === 'running' || taskStatus === 'pending') {
					idleTimer = setTimeout(() => { if (typeof quinoaNotify === 'function') quinoaNotify('Agente aguardando?', repoLabel + ' — sem atividade há 3 min', location.href); }, IDLE_MS);
				}
			}
			document.body.addEventListener('htmx:afterSettle', function() {
				const badge = document.getElementById('status-badge');
				if (!badge) return;
				const newStatus = badge.textContent.trim();
				if (taskStatus !== newStatus && (newStatus === 'idle' || newStatus === 'done' || newStatus === 'error' || newStatus === 'stopped')) {
					clearTimeout(idleTimer);
					if (typeof quinoaNotify === 'function') quinoaNotify('Agente finalizado', repoLabel, location.href);
					taskStatus = newStatus;
				}
			});
			const term = new Terminal({
				cursorBlink: true, fontSize: 14,
				fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', Consolas, monospace",
				scrollback: 5000,
				theme: {
					background: '#0d1117', foreground: '#e6edf3', cursor: '#58a6ff', cursorAccent: '#0d1117',
					black: '#0d1117', red: '#f85149', green: '#3fb950', yellow: '#d29922',
					blue: '#58a6ff', magenta: '#bc8cff', cyan: '#39c5cf', white: '#e6edf3',
					brightBlack: '#6e7681', brightRed: '#ff7b72', brightGreen: '#56d364',
					brightYellow: '#e3b341', brightBlue: '#79c0ff', brightMagenta: '#d2a8ff',
					brightCyan: '#56d4dd', brightWhite: '#f0f6fc',
				}
			});
			const fitAddon = new FitAddon.FitAddon();
			const linkAddon = new WebLinksAddon.WebLinksAddon();
			term.loadAddon(fitAddon); term.loadAddon(linkAddon);
			term.open(wrap); fitAddon.fit(); term.focus();
			function showBanner(msg, isError) { banner.textContent = msg; banner.className = 'conn-banner visible' + (isError ? ' error' : ''); }
			function hideBanner() { banner.className = 'conn-banner'; }
			let ws, reconnectTimer, intentionallyClosed = false;
			function connect() {
				const proto = location.protocol === 'https:' ? 'wss' : 'ws';
				ws = new WebSocket(proto + '://' + location.host + wsUrl);
				ws.binaryType = 'arraybuffer';
				ws.onopen = () => { hideBanner(); fitAddon.fit(); sendResize(); term.focus(); resetIdle(); };
				ws.onmessage = e => { term.write(e.data instanceof ArrayBuffer ? new Uint8Array(e.data) : e.data); resetIdle(); };
				ws.onclose = () => {
					clearTimeout(idleTimer);
					if (intentionallyClosed) return;
					if (taskStatus === 'done' || taskStatus === 'error' || taskStatus === 'stopped') showBanner('Sessão encerrada', false);
					else { showBanner('Reconectando...', false); reconnectTimer = setTimeout(connect, 2000); }
				};
				ws.onerror = () => showBanner('Erro de conexão', true);
			}
			function sendResize() { if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows })); }
			term.onData(data => { if (ws && ws.readyState === WebSocket.OPEN) ws.send(new TextEncoder().encode(data)); });
			term.onResize(() => sendResize());
			window.addEventListener('resize', () => fitAddon.fit());
			setTimeout(connect, status === 'pending' ? 1500 : 0);
		})();
		</script>
	}
}

func statusTrigger(status string) string {
	if status == "running" || status == "pending" || status == "idle" { return "every 2s" }
	return "none"
}
```

## FILE: backend/templates/board.templ

```templ
package templates

import (
	"fmt"

	"github.com/wiltsou/quinoa/internal/db"
)

templ Board(todo, doing, review, done []*db.Story) {
	@Layout("Board") {
		<style>
			.board-body { height: calc(100vh - 52px); display: flex; flex-direction: column; overflow: hidden; }
			#board-poll { flex: 1; min-height: 0; overflow: hidden; }
			.board { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; padding: 16px; height: 100%; box-sizing: border-box; }
			.col { background: var(--bg2); border: 1px solid var(--border); border-radius: var(--radius); display: flex; flex-direction: column; min-height: 0; overflow: hidden; }
			.col-header { display: flex; align-items: center; gap: 8px; padding: 12px 14px; border-bottom: 1px solid var(--border); flex-shrink: 0; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .5px; color: var(--muted); }
			.col-count { background: var(--bg3); border-radius: 20px; padding: 1px 7px; font-size: 11px; font-weight: 600; }
			.col-spacer { flex: 1; }
			.col-add { padding: 1px 8px; font-size: 18px; line-height: 1.2; color: var(--muted); background: none; border: 1px solid transparent; cursor: pointer; border-radius: var(--radius); }
			.col-add:hover { background: var(--bg3); color: var(--text); border-color: var(--border); }
			.col-body { flex: 1; overflow-y: auto; padding: 10px; display: flex; flex-direction: column; gap: 8px; }
			.col-empty { color: var(--muted); font-size: 12px; text-align: center; padding: 24px 12px; }
			.story-card { background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius); padding: 12px; }
			.story-card-title { font-weight: 600; font-size: 13px; line-height: 1.4; margin-bottom: 5px; }
			.story-card-desc { font-size: 12px; color: var(--muted); line-height: 1.5; margin-bottom: 8px; overflow: hidden; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; white-space: pre-wrap; }
			.story-card-status { margin-bottom: 8px; }
			.story-card-actions { display: flex; align-items: center; gap: 5px; flex-wrap: wrap; }
			.modal-overlay { display: none; position: fixed; inset: 0; z-index: 200; background: rgba(0,0,0,.6); align-items: center; justify-content: center; }
			.modal-overlay.open { display: flex; }
			.modal { background: var(--bg2); border: 1px solid var(--border); border-radius: var(--radius); width: 560px; max-width: 95vw; max-height: 90vh; display: flex; flex-direction: column; overflow: hidden; }
			.modal-header { padding: 14px 16px; border-bottom: 1px solid var(--border); display: flex; align-items: center; flex-shrink: 0; }
			.modal-title { font-weight: 600; font-size: 14px; flex: 1; }
			.modal-close { background: none; border: none; color: var(--muted); font-size: 20px; cursor: pointer; padding: 0 2px; line-height: 1; }
			.modal-close:hover { color: var(--text); }
			.modal-body { padding: 16px; overflow-y: auto; }
			.modal-footer { padding: 12px 16px; border-top: 1px solid var(--border); display: flex; justify-content: flex-end; gap: 8px; flex-shrink: 0; }
			.story-ref { margin: 12px 16px 0; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg3); }
			.story-ref-title { font-weight: 600; font-size: 13px; margin-bottom: 4px; }
			.story-ref-desc { font-size: 12px; color: var(--muted); line-height: 1.5; overflow: hidden; display: -webkit-box; -webkit-line-clamp: 4; -webkit-box-orient: vertical; white-space: pre-wrap; }
			.dir-modal-overlay { display: none; position: fixed; inset: 0; z-index: 300; background: rgba(0,0,0,.6); align-items: center; justify-content: center; }
			.dir-modal-overlay.open { display: flex; }
			.dir-modal { background: var(--bg2); border: 1px solid var(--border); border-radius: var(--radius); width: 520px; max-width: 95vw; display: flex; flex-direction: column; max-height: 70vh; }
			.dir-modal-header { padding: 14px 16px; border-bottom: 1px solid var(--border); display: flex; align-items: center; gap: 8px; }
			.dir-modal-header .dir-path { font-family: var(--font-mono); font-size: 12px; color: var(--muted); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
			.dir-modal-list { flex: 1; overflow-y: auto; padding: 8px 0; }
			.dir-entry { padding: 8px 16px; cursor: pointer; font-family: var(--font-mono); font-size: 13px; display: flex; align-items: center; gap: 8px; }
			.dir-entry:hover { background: var(--bg3); }
			.dir-entry::before { content: '📁'; font-size: 14px; }
			.dir-entry.up::before { content: '↑'; font-style: normal; font-size: 14px; }
			.dir-empty { padding: 24px 16px; color: var(--muted); font-size: 13px; text-align: center; }
			.dir-modal-footer { padding: 12px 16px; border-top: 1px solid var(--border); display: flex; justify-content: flex-end; gap: 8px; }
			.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
			.path-picker { display: flex; align-items: center; gap: 8px; }
			.path-picker-label { font-size: 12px; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
			.btn-sm { padding: 3px 10px !important; font-size: 11px !important; }
			.col-review { border-color: rgba(88,166,255,.25); }
			.col-review .col-header { color: var(--accent); }
			@keyframes pulse-dot { 0%, 100% { opacity: 1; } 50% { opacity: .35; } }
			.badge.running::before { animation: pulse-dot 1.5s ease-in-out infinite; }
		</style>
		<div class="topbar">
			<a href="/" class="logo">quinoa</a>
			@Nav("board")
		</div>
		<div id="new-story-overlay" class="modal-overlay" onclick="if(this===event.target)closeModal('new-story-overlay')">
			<div class="modal">
				<div class="modal-header">
					<span class="modal-title">Nova História</span>
					<button class="modal-close" onclick="closeModal('new-story-overlay')">×</button>
				</div>
				<form hx-post="/stories" hx-target="body">
					<div class="modal-body">
						<div class="field">
							<label>Título</label>
							<input type="text" name="title" required placeholder="Ex: Implementar autenticação OAuth..."/>
						</div>
						<div class="field">
							<label>Descrição / Prompt do Agente</label>
							<textarea name="description" rows="6" placeholder="Descreva a tarefa em detalhes."></textarea>
						</div>
					</div>
					<div class="modal-footer">
						<button type="button" onclick="closeModal('new-story-overlay')">Cancelar</button>
						<button type="submit" class="primary">Criar</button>
					</div>
				</form>
			</div>
		</div>
		<div id="start-modal-overlay" class="modal-overlay" onclick="if(this===event.target)closeModal('start-modal-overlay')">
			<div class="modal">
				<div class="modal-header">
					<span class="modal-title">Iniciar Agente</span>
					<button class="modal-close" onclick="closeModal('start-modal-overlay')">×</button>
				</div>
				<div class="story-ref">
					<div id="start-ref-title" class="story-ref-title"></div>
					<div id="start-ref-desc" class="story-ref-desc"></div>
				</div>
				<form hx-post="/stories/start" hx-target="body">
					<input type="hidden" id="start-story-id" name="story_id"/>
					<div class="modal-body">
						<div class="form-row">
							<div class="field">
								<label>URL do repositório (git clone)</label>
								<input type="text" name="repo_url" placeholder="https://github.com/user/repo.git"/>
							</div>
							<div class="field">
								<label>Pasta local</label>
								<div class="path-picker">
									<input type="hidden" id="start-repo-path" name="repo_path"/>
									<span id="start-path-label" class="path-picker-label" style="color:var(--muted)">Nenhuma pasta selecionada</span>
									<button type="button" class="btn" style="white-space:nowrap;flex-shrink:0" onclick="openDirModal()">Selecionar pasta</button>
								</div>
							</div>
						</div>
						<div class="form-row">
							<div class="field">
								<label>Branch (opcional)</label>
								<input type="text" name="repo_branch" placeholder="main"/>
							</div>
							<div class="field">
								<label>Agente</label>
								<select id="start-agent-preset" onchange="onAgentChange(this)">
									<option value="claude --dangerously-skip-permissions" selected>Claude Code</option>
									<option value="aider --yes-always">Aider</option>
									<option value="bash">Bash (manual)</option>
									<option value="">Personalizado…</option>
								</select>
								<input type="hidden" id="start-agent-cmd" name="agent_command" value="claude --dangerously-skip-permissions"/>
								<input type="text" id="start-agent-custom" placeholder="ex: meu-agente --flag" style="display:none;margin-top:6px"/>
							</div>
						</div>
						<div class="field">
							<label>Variáveis de ambiente extras (uma por linha: KEY=VALUE)</label>
							<textarea name="env_extra" rows="3" placeholder="ANTHROPIC_API_KEY=sk-ant-...&#10;CUSTOM_VAR=value"></textarea>
						</div>
					</div>
					<div class="modal-footer">
						<button type="button" onclick="closeModal('start-modal-overlay')">Cancelar</button>
						<button type="submit" class="primary">▶ Iniciar</button>
					</div>
				</form>
			</div>
		</div>
		<div id="dir-modal-overlay" class="dir-modal-overlay" onclick="onDirOverlayClick(event)">
			<div class="dir-modal">
				<div class="dir-modal-header">
					<button type="button" class="btn" onclick="dirUp()" id="dir-up-btn" title="Pasta acima">↑</button>
					<span class="dir-path" id="dir-current-path"></span>
				</div>
				<div class="dir-modal-list" id="dir-list"></div>
				<div class="dir-modal-footer">
					<button type="button" onclick="closeDirModal()">Cancelar</button>
					<button type="button" class="primary" onclick="confirmDir()">Selecionar esta pasta</button>
				</div>
			</div>
		</div>
		<div class="board-body">
			<div id="board-poll" hx-get="/api/board" hx-trigger="every 3s" hx-swap="innerHTML">
				@BoardCols(todo, doing, review, done)
			</div>
		</div>
		<script>
		let pollPaused = false;
		htmx.on('#board-poll', 'htmx:beforeRequest', evt => { if (pollPaused) evt.preventDefault(); });
		function openModal(id) { pollPaused = true; document.getElementById(id).classList.add('open'); }
		function closeModal(id) { document.getElementById(id).classList.remove('open'); pollPaused = false; }
		var _prevStatus = {};
		htmx.on('#board-poll', 'htmx:afterSettle', function() {
			document.querySelectorAll('.story-card[data-story-id]').forEach(card => {
				const id = card.dataset.storyId, status = card.dataset.taskStatus || '';
				const title = (card.querySelector('.story-card-title') || {}).textContent || id;
				const prev = _prevStatus[id];
				if (prev === 'running' && (status === 'idle' || status === 'error' || status === 'stopped'))
					if (typeof quinoaNotify === 'function') quinoaNotify('Agente finalizado', title, '/board');
				if (status) _prevStatus[id] = status;
			});
		});
		function openStartModal(btn) {
			document.getElementById('start-story-id').value = btn.dataset.storyId;
			document.getElementById('start-ref-title').textContent = btn.dataset.storyTitle;
			document.getElementById('start-ref-desc').textContent = btn.dataset.storyDesc || '';
			document.getElementById('start-agent-preset').value = 'claude --dangerously-skip-permissions';
			document.getElementById('start-agent-cmd').value = 'claude --dangerously-skip-permissions';
			document.getElementById('start-agent-custom').style.display = 'none';
			openModal('start-modal-overlay');
		}
		function onAgentChange(sel) {
			const cmd = document.getElementById('start-agent-cmd'), custom = document.getElementById('start-agent-custom');
			if (sel.value !== '') { cmd.value = sel.value; custom.style.display = 'none'; }
			else { cmd.value = ''; custom.style.display = ''; custom.focus(); custom.oninput = () => cmd.value = custom.value; }
		}
		let dirCurrentPath = '', dirParentPath = '';
		function openDirModal() { navigateTo(document.getElementById('start-repo-path').value || null); document.getElementById('dir-modal-overlay').classList.add('open'); }
		function closeDirModal() { document.getElementById('dir-modal-overlay').classList.remove('open'); }
		function onDirOverlayClick(e) { if (e.target === document.getElementById('dir-modal-overlay')) closeDirModal(); }
		function dirUp() { if (dirParentPath) navigateTo(dirParentPath); }
		function navigateTo(path) {
			fetch('/api/fs' + (path ? '?path=' + encodeURIComponent(path) : '')).then(r => r.json()).then(renderDir).catch(() => { document.getElementById('dir-list').innerHTML = '<div class="dir-empty">Erro ao listar pasta</div>'; });
		}
		function renderDir(data) {
			dirCurrentPath = data.path; dirParentPath = data.parent || '';
			document.getElementById('dir-current-path').textContent = data.path;
			document.getElementById('dir-up-btn').disabled = !dirParentPath;
			const list = document.getElementById('dir-list');
			list.innerHTML = '';
			if (!data.dirs || data.dirs.length === 0) { list.innerHTML = '<div class="dir-empty">Nenhuma subpasta encontrada</div>'; return; }
			data.dirs.forEach(entry => { const el = document.createElement('div'); el.className = 'dir-entry'; el.textContent = entry.name; el.onclick = () => navigateTo(entry.path); list.appendChild(el); });
		}
		function confirmDir() {
			document.getElementById('start-repo-path').value = dirCurrentPath;
			document.getElementById('start-path-label').textContent = dirCurrentPath;
			document.getElementById('start-path-label').style.color = 'var(--text)';
			closeDirModal();
		}
		</script>
	}
}

templ BoardCols(todo, doing, review, done []*db.Story) {
	<div class="board">
		<div class="col">
			<div class="col-header">
				<span>Todo</span>
				<span class="col-count">{ fmt.Sprintf("%d", len(todo)) }</span>
				<div class="col-spacer"></div>
				<button class="col-add" title="Nova história" onclick="openModal('new-story-overlay')">+</button>
			</div>
			<div class="col-body">
				if len(todo) == 0 { <div class="col-empty">Nenhuma história. Clique em + para criar.</div> }
				for _, s := range todo { @storyCard(s) }
			</div>
		</div>
		<div class="col">
			<div class="col-header">
				<span>Doing</span>
				<span class="col-count">{ fmt.Sprintf("%d", len(doing)) }</span>
			</div>
			<div class="col-body">
				if len(doing) == 0 { <div class="col-empty">Nenhuma história em execução.</div> }
				for _, s := range doing { @storyCard(s) }
			</div>
		</div>
		<div class="col col-review">
			<div class="col-header">
				<span>Review</span>
				<span class="col-count">{ fmt.Sprintf("%d", len(review)) }</span>
			</div>
			<div class="col-body">
				if len(review) == 0 { <div class="col-empty">Nenhum item aguardando revisão.</div> }
				for _, s := range review { @storyCard(s) }
			</div>
		</div>
		<div class="col">
			<div class="col-header">
				<span>Done</span>
				<span class="col-count">{ fmt.Sprintf("%d", len(done)) }</span>
			</div>
			<div class="col-body">
				if len(done) == 0 { <div class="col-empty">Nenhuma história concluída.</div> }
				for _, s := range done { @storyCard(s) }
			</div>
		</div>
	</div>
}

templ storyCard(s *db.Story) {
	<div class="story-card" data-story-id={ s.ID } data-task-status={ s.TaskStatus }>
		<div class="story-card-title">{ s.Title }</div>
		if s.Description != "" { <div class="story-card-desc">{ s.Description }</div> }
		if s.TaskStatus != "" { <div class="story-card-status">@StatusBadge(s.TaskStatus)</div> }
		<div class="story-card-actions">
			switch s.KanbanStatus {
			case "todo":
				<button class="btn btn-sm primary" data-story-id={ s.ID } data-story-title={ s.Title } data-story-desc={ s.Description } onclick="openStartModal(this)">▶ Iniciar</button>
				<button class="btn btn-sm danger" hx-delete={ "/stories/" + s.ID } hx-confirm="Excluir esta história?">×</button>
			case "doing":
				if s.TaskID != "" { <a href={ templ.SafeURL("/tasks/" + s.TaskID) } class="btn btn-sm">→ Terminal</a> }
				<button class="btn btn-sm" hx-post={ "/stories/" + s.ID + "/move" } hx-vals={ `{"to":"todo"}` }>✕ Parar</button>
			case "review":
				if s.TaskID != "" { <a href={ templ.SafeURL("/tasks/" + s.TaskID) } class="btn btn-sm">→ Terminal</a> }
				<button class="btn btn-sm primary" hx-post={ "/stories/" + s.ID + "/move" } hx-vals={ `{"to":"done"}` }>✓ Aprovar</button>
				<button class="btn btn-sm" hx-post={ "/stories/" + s.ID + "/move" } hx-vals={ `{"to":"doing"}` }>✎ Corrigir</button>
				<button class="btn btn-sm" hx-post={ "/stories/" + s.ID + "/move" } hx-vals={ `{"to":"todo"}` }>↩ Reabrir</button>
			case "done":
				if s.TaskID != "" { <a href={ templ.SafeURL("/tasks/" + s.TaskID) } class="btn btn-sm">→ Terminal</a> }
				<button class="btn btn-sm" hx-post={ "/stories/" + s.ID + "/move" } hx-vals={ `{"to":"todo"}` }>↩ Reabrir</button>
				<button class="btn btn-sm danger" hx-delete={ "/stories/" + s.ID } hx-confirm="Excluir esta história?">×</button>
			}
		</div>
	</div>
}
```

## FILE: backend/Dockerfile

```dockerfile
FROM golang:1.23-alpine AS build
RUN apk add --no-cache git curl
RUN go install github.com/a-h/templ/cmd/templ@v0.2.793
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN templ generate ./templates/...
RUN go build -o /quinoa .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /quinoa /quinoa
EXPOSE 8080
CMD ["/quinoa"]
```

## FILE: sandbox/Dockerfile

```dockerfile
FROM node:20-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    git curl ca-certificates python3 python3-pip python3-venv jq sudo \
    && rm -rf /var/lib/apt/lists/*

RUN echo "node ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/node && \
    chmod 440 /etc/sudoers.d/node

RUN npm install -g @anthropic-ai/claude-code

RUN mkdir -p /workspace /run/claude-host-creds && \
    chmod 755 /run/claude-host-creds && \
    chown node:node /workspace

COPY runner.sh /usr/local/bin/runner
RUN chmod +x /usr/local/bin/runner

USER node
ENV HOME=/home/node
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/runner"]
```

## FILE: sandbox/runner.sh

```bash
#!/usr/bin/env bash
set -euo pipefail

export HOME="${HOME:-/home/node}"
WORK_DIR="${WORK_DIR:-/workspace/repo}"

if [ -d /run/claude-host-creds/claude ]; then
    mkdir -p "$HOME/.claude"
    cp -rn /run/claude-host-creds/claude/. "$HOME/.claude/" 2>/dev/null || true
fi
if [ -f /run/claude-host-creds/claude.json ]; then
    cp /run/claude-host-creds/claude.json "$HOME/.claude.json" 2>/dev/null || true
fi

echo "[quinoa] configurando credenciais do agente..."

if [ -f "$HOME/.claude.json" ]; then
    jq --arg p "$WORK_DIR" \
        '(.projects[$p] // {}) + {"hasTrustDialogAccepted":true} as $proj
         | .projects = ((.projects // {}) + {($p): $proj})' \
        "$HOME/.claude.json" > /tmp/.cj.tmp && mv /tmp/.cj.tmp "$HOME/.claude.json" || true
else
    printf '{"projects":{"%s":{"hasTrustDialogAccepted":true}}}\n' "$WORK_DIR" > "$HOME/.claude.json"
fi

echo "[quinoa] iniciando setup..."

if [[ -n "${REPO_URL:-}" ]]; then
    echo "[quinoa] clonando $REPO_URL"
    if [[ -n "${REPO_BRANCH:-}" ]]; then
        git clone --depth=1 --branch "$REPO_BRANCH" "$REPO_URL" "$WORK_DIR"
    else
        git clone --depth=1 "$REPO_URL" "$WORK_DIR"
    fi
elif [[ -d "/workspace/repo" ]]; then
    echo "[quinoa] usando repo montado em /workspace/repo"
    WORK_DIR="/workspace/repo"
else
    echo "[quinoa] ERRO: nenhum REPO_URL definido e nenhum repo montado em /workspace/repo"
    exit 1
fi

cd "$WORK_DIR"
echo "[quinoa] setup concluído — iniciando agente..."
echo ""

cat >> CLAUDE.md 2>/dev/null << 'QUINOA_INSTRUCTIONS'

## Quinoa Board Integration (required)
When you have **fully completed** the requested task, you MUST execute the following
command as your very last action before stopping:
```bash
echo "[QUINOA:DONE]"
```
This signals the quinoa board to move the task card to the Review column.
If the user asks you to make corrections, work on them and run the command again when done.
QUINOA_INSTRUCTIONS

exec bash -c "$AGENT_COMMAND"
```

## FILE: docker-compose.yml

```yaml
services:
  backend:
    build:
      context: ./backend
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - quinoa-data:/data
    environment:
      - PORT=8080
      - DATA_DIR=/data
    restart: unless-stopped

volumes:
  quinoa-data:
```

## FILE: Makefile

```makefile
.PHONY: all dev build sandbox templ deps clean run

all: sandbox deps templ build

sandbox:
	docker build -t quinoa-sandbox ./sandbox

deps:
	cd backend && go mod tidy

templ:
	cd backend && templ generate ./templates/...

build:
	cd backend && go build -o ../bin/quinoa .

run: build
	./bin/quinoa

dev: sandbox
	cd backend && templ generate ./templates/... && go run .

serve: build
	QUINOA_HEADLESS=1 ./bin/quinoa

release-linux:
	cd backend && GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ../bin/quinoa-linux-amd64 .

release-macos-intel:
	cd backend && GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o ../bin/quinoa-macos-amd64 .

release-macos-arm:
	cd backend && GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o ../bin/quinoa-macos-arm64 .

release-windows:
	cd backend && GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o ../bin/quinoa-windows-amd64.exe .

release: templ release-linux release-macos-intel release-macos-arm release-windows

clean:
	rm -rf bin/
	docker rmi quinoa-sandbox 2>/dev/null || true
```

---

## Pré-requisitos

- Go 1.23+ (`go install` ou via asdf)
- Docker Engine com socket em `/var/run/docker.sock`
- templ CLI: `go install github.com/a-h/templ/cmd/templ@v0.2.793`
- Para usar Claude Code: `~/.claude` e `~/.claude.json` configurados (`claude auth login`)

## Como construir e executar

```bash
cd quinoa
make deps        # go mod tidy
make templ       # gera *_templ.go a partir dos .templ
make sandbox     # constrói imagem Docker quinoa-sandbox
make build       # compila o binário ./bin/quinoa
make serve       # executa em modo headless (sem abrir browser)
# ou
make run         # executa e abre browser automaticamente
```

## Notas importantes

- `go.sum` é gerado automaticamente por `go mod tidy` — não criar manualmente
- Arquivos `*_templ.go` são gerados por `templ generate` — não criar manualmente
- O módulo Go é `github.com/wiltsou/quinoa` — pode adaptar para outro username se necessário
- Dados persistidos em `~/.quinoa/quinoa.db` (SQLite com WAL)
- Containers de agentes são removidos automaticamente após 2 horas
- Variáveis de ambiente: `PORT`, `DATA_DIR`, `QUINOA_HEADLESS=1`, `ANTHROPIC_API_KEY`
