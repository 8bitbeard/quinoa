.PHONY: all dev build sandbox templ deps clean

# Default: build everything
all: sandbox deps templ build

# Build the sandbox Docker image (agent execution environment)
sandbox:
	docker build -t quinoa-sandbox ./sandbox

# Download Go dependencies and generate go.sum
deps:
	cd backend && go mod tidy

# Generate Go code from .templ files (requires templ CLI)
# Install: go install github.com/a-h/templ/cmd/templ@v0.2.793
templ:
	cd backend && templ generate ./templates/...

# Build the backend binary
build:
	cd backend && go build -o ../bin/quinoa .

# Run in development mode (auto-reloads on file changes if air is installed)
# Install air: go install github.com/cosmtrek/air@latest
dev: sandbox
	cd backend && templ generate ./templates/... && go run .

# Run via Docker Compose (production-like)
up:
	docker compose up --build

# Remove built artifacts
clean:
	rm -rf bin/
	docker rmi quinoa-sandbox 2>/dev/null || true
