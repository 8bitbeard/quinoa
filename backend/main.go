package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wiltsou/quinoa/internal/db"
	"github.com/wiltsou/quinoa/internal/docker"
	"github.com/wiltsou/quinoa/internal/tui"
)

func setupLogging(dataDir string) func() {
	logPath := filepath.Join(dataDir, "quinoa.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return func() {}
	}
	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("quinoa started (log: %s)", logPath)
	return func() { f.Close() }
}

func main() {
	dataDir := dataDirectory()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	closeLog := setupLogging(dataDir)
	defer closeLog()

	database, err := db.New(filepath.Join(dataDir, "quinoa.db"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Fatalf("docker: %v", err)
	}

	runner := tui.NewRunner(database, dockerClient)
	model := tui.NewModel(database, runner)

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
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
