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
