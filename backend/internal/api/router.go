package api

import (
	"net/http"

	"github.com/wiltsou/quinoa/internal/db"
	"github.com/wiltsou/quinoa/internal/docker"
)

func NewRouter(database *db.DB, dockerClient *docker.Client) http.Handler {
	h := &handler{db: database, docker: dockerClient}

	mux := http.NewServeMux()

	// Pages (server-rendered HTML)
	mux.HandleFunc("GET /{$}", h.indexPage)
	mux.HandleFunc("GET /tasks/{id}", h.taskPage)
	mux.HandleFunc("GET /board", h.boardPage)

	// Task actions
	mux.HandleFunc("POST /tasks", h.createTask)
	mux.HandleFunc("DELETE /tasks/{id}", h.stopTask)

	// Story actions (literal routes before wildcard)
	mux.HandleFunc("POST /stories", h.createStory)
	mux.HandleFunc("POST /stories/start", h.startStory)
	mux.HandleFunc("POST /stories/{id}/move", h.moveStory)
	mux.HandleFunc("DELETE /stories/{id}", h.deleteStory)

	// HTMX fragments
	mux.HandleFunc("GET /api/tasks", h.taskListFragment)
	mux.HandleFunc("GET /api/tasks/{id}/status", h.taskStatusFragment)
	mux.HandleFunc("GET /api/board", h.boardFragment)
	mux.HandleFunc("GET /api/fs", h.listDirectory)

	// WebSocket PTY bridge
	mux.HandleFunc("GET /ws/tasks/{id}/terminal", h.terminalWS)

	// Health check
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
