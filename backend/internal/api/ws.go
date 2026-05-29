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

// GET /ws/tasks/{id}/terminal
//
// Bridges a WebSocket connection to the Docker container's PTY.
//
// Protocol (client → server):
//   - Binary frame  : raw keystrokes / input bytes
//   - Text frame    : JSON {"type":"resize","cols":N,"rows":N}
//
// Protocol (server → client):
//   - Binary frame  : raw PTY output bytes
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

	// Send buffered PTY history so the client sees previous output on connect.
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

	// Docker PTY → WebSocket
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

	// WebSocket → Docker PTY (blocks until client disconnects)
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
