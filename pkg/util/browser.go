package util

import (
	"os/exec"
	"runtime"
)

// OpenBrowser opens the specified URL in the default browser.
// Returns an error if the browser could not be opened.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		// Try xdg-open first, fall back to common browsers
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		// Try xdg-open as a fallback for other Unix-like systems
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}
