package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// Open launches the given URL in a Chromium-based browser using --app mode
// (clean window, no address bar). Falls back to the system default browser.
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
	"google-chrome",
	"google-chrome-stable",
	"chromium",
	"chromium-browser",
	"microsoft-edge",
	"microsoft-edge-stable",
	"brave-browser",
}

var appFlags = []string{
	"--no-first-run",
	"--no-default-browser-check",
	"--window-size=1400,900",
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
	winCandidates := []string{"msedge", "chrome", "chromium"}
	for _, bin := range winCandidates {
		if path, err := exec.LookPath(bin); err == nil {
			args := append([]string{"--app=" + url}, appFlags...)
			return exec.Command(path, args...).Start()
		}
	}
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
