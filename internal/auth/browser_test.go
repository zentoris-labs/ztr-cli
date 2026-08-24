package auth

import (
	"runtime"
	"testing"
)

func clearBrowserEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"SSH_CONNECTION", "SSH_TTY", "DISPLAY", "WAYLAND_DISPLAY"} {
		t.Setenv(k, "")
	}
}

func TestIsHeadlessSSH(t *testing.T) {
	clearBrowserEnv(t)
	t.Setenv("SSH_CONNECTION", "10.0.0.1 2222 10.0.0.2 22")
	if !IsHeadless() {
		t.Error("an SSH session should be headless regardless of OS")
	}
}

func TestIsHeadlessDisplayHeuristic(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skipf("the DISPLAY heuristic is Linux/BSD only; GOOS=%s is assumed to have a GUI", runtime.GOOS)
	}
	clearBrowserEnv(t)
	if !IsHeadless() {
		t.Error("no SSH and no DISPLAY/WAYLAND should be headless")
	}
	t.Setenv("DISPLAY", ":0")
	if IsHeadless() {
		t.Error("a set DISPLAY should not be headless")
	}
}

func TestIsHeadlessGUIOS(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skipf("GUI is assumed only on darwin/windows; GOOS=%s", runtime.GOOS)
	}
	clearBrowserEnv(t)
	if IsHeadless() {
		t.Error("darwin/windows without SSH should be assumed to have a GUI")
	}
}
