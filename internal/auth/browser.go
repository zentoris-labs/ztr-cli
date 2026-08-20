package auth

import (
	"os"
	"os/exec"
	"runtime"
)

// IsHeadless reports whether this looks like a box with no local browser to drive a loopback
// sign-in: an SSH session, or a Linux/BSD host with no X11 / Wayland display. macOS and Windows
// are assumed to have a usable GUI session. Used to auto-pick the device flow (D7); `--device`
// forces it regardless.
func IsHeadless() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return true
	}
	switch runtime.GOOS {
	case "darwin", "windows":
		return false
	default:
		return os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == ""
	}
}

// openBrowser tries to open url in the user's default browser. A failure is non-fatal:
// callers print the URL as a fallback.
func openBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		name = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(name, args...).Start()
}
