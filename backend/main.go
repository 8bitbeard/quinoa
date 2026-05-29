package main

import (
	"log"
	"net/http"
	"os"

	"github.com/wiltsou/quinoa/internal/api"
	"github.com/wiltsou/quinoa/internal/db"
	"github.com/wiltsou/quinoa/internal/docker"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "."
	}

	database, err := db.New(dataDir + "/quinoa.db")
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Fatalf("docker: %v", err)
	}

	router := api.NewRouter(database, dockerClient)

	log.Printf("quinoa listening on :%s", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatal(err)
	}
}
