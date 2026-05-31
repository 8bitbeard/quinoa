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

# Run in desktop mode (opens browser window automatically)
run: build
	./bin/quinoa

# Development: regenerate templates and run with auto-open browser
dev: sandbox
	cd backend && templ generate ./templates/... && go run .

# Run as headless HTTP server (no browser auto-open)
serve: build
	QUINOA_HEADLESS=1 ./bin/quinoa

# Cross-compile for Linux amd64
release-linux:
	cd backend && GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ../bin/quinoa-linux-amd64 .

# Cross-compile for macOS amd64 (Intel)
release-macos-intel:
	cd backend && GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o ../bin/quinoa-macos-amd64 .

# Cross-compile for macOS arm64 (Apple Silicon)
release-macos-arm:
	cd backend && GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o ../bin/quinoa-macos-arm64 .

# Cross-compile for Windows amd64
release-windows:
	cd backend && GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o ../bin/quinoa-windows-amd64.exe .

# Build all release targets
release: templ release-linux release-macos-intel release-macos-arm release-windows

clean:
	rm -rf bin/
	docker rmi quinoa-sandbox 2>/dev/null || true
