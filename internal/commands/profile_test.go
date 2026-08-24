package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/zentoris-labs/ztr-cli/internal/auth"
)

func TestCorruptStateFallsBackToDefault(t *testing.T) {
	isolate(t)
	// A present-but-unparseable config.json must not brick the CLI: commands should warn and
	// resolve to "default" (so `auth login`/`auth switch` can still rewrite the state).
	dir := filepath.Join(os.Getenv("HOME"), ".zentoris")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := runRoot(t, "auth", "status")
	if !strings.Contains(out, "Active profile: default") {
		t.Fatalf("output %q, want fallback to \"default\" on corrupt state", out)
	}
	if !strings.Contains(out, "could not read the active profile") {
		t.Fatalf("output %q, want a warning about the unreadable state", out)
	}
}

// isolate points HOME at a temp dir, mocks the keychain, and clears every ZENTORIS_/CI env var, so
// a command-execution test neither touches the real user state nor reaches the network.
func isolate(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	for _, k := range []string{
		"ZENTORIS_PROFILE", "ZENTORIS_TOKEN", "ZENTORIS_CLIENT_ID", "ZENTORIS_CLIENT_SECRET",
		"ZENTORIS_DOMAIN", "ZENTORIS_OIDC_TOKEN", "ZENTORIS_OIDC_TOKEN_FILE",
		"ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN",
	} {
		t.Setenv(k, "")
	}
}

func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	root.SilenceUsage, root.SilenceErrors = true, true
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func seedProfile(t *testing.T, name string) {
	t.Helper()
	creds := &auth.Credentials{AccessToken: "tok-" + name + "-long", Expiry: time.Now().Add(time.Hour)}
	if err := auth.NewStore().Save(name, creds); err != nil {
		t.Fatal(err)
	}
	if err := auth.RegisterProfile(name); err != nil {
		t.Fatal(err)
	}
}

func TestProfileResolutionFromFlag(t *testing.T) {
	isolate(t)
	out, _ := runRoot(t, "--profile", "foo", "auth", "status")
	if !strings.Contains(out, "Active profile: foo") {
		t.Fatalf("output %q, want 'Active profile: foo'", out)
	}
}

func TestProfileResolutionFromEnv(t *testing.T) {
	isolate(t)
	t.Setenv("ZENTORIS_PROFILE", "bar")
	out, _ := runRoot(t, "auth", "status")
	if !strings.Contains(out, "Active profile: bar") {
		t.Fatalf("output %q, want 'Active profile: bar'", out)
	}
}

func TestProfileResolutionFromActiveDefault(t *testing.T) {
	isolate(t)
	seedProfile(t, "act")
	if err := auth.SwitchProfile("act"); err != nil {
		t.Fatal(err)
	}
	out, _ := runRoot(t, "auth", "status")
	if !strings.Contains(out, "Active profile: act") {
		t.Fatalf("output %q, want 'Active profile: act' from the persisted active profile", out)
	}
	if !strings.Contains(out, "Authenticated via login") {
		t.Fatalf("output %q, want it to resolve the seeded login", out)
	}
}

func TestProfileResolutionFallsBackToDefault(t *testing.T) {
	isolate(t)
	out, _ := runRoot(t, "auth", "status")
	if !strings.Contains(out, "Active profile: default") {
		t.Fatalf("output %q, want 'Active profile: default'", out)
	}
	if !strings.Contains(out, "Not authenticated") {
		t.Fatalf("output %q, want 'Not authenticated' with no credentials", out)
	}
}

func TestAuthLoginFlags(t *testing.T) {
	cmd := newAuthLoginCmd(&deps{})
	for _, name := range []string{"activate", "use-device-code"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("auth login should register the --%s flag", name)
		}
	}
}

func TestPassiveLoginDoesNotActivate(t *testing.T) {
	isolate(t)
	seedProfile(t, "work") // simulates a passive `auth login --profile work` (stored + registered, not switched)
	out, _ := runRoot(t, "auth", "status")
	if !strings.Contains(out, "Active profile: default") {
		t.Fatalf("output %q, want the active profile to stay 'default' - login must not auto-activate", out)
	}
}

func TestAuthListAndSwitch(t *testing.T) {
	isolate(t)
	seedProfile(t, "work")
	seedProfile(t, "personal")
	if err := auth.SwitchProfile("personal"); err != nil { // active is set only by an explicit switch
		t.Fatal(err)
	}

	out, err := runRoot(t, "auth", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "* personal") || !strings.Contains(out, "work") {
		t.Fatalf("list output %q, want personal active and work present", out)
	}

	out, err = runRoot(t, "auth", "switch", "work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `Active profile is now "work"`) {
		t.Fatalf("switch output %q", out)
	}
	if active, err := auth.ActiveProfile(); err != nil || active != "work" {
		t.Fatalf("active = %q (err %v), want work after switch", active, err)
	}

	out, _ = runRoot(t, "auth", "list")
	if !strings.Contains(out, "* work") {
		t.Fatalf("list output %q, want work now active", out)
	}
}

func TestAuthSwitchUnknownProfile(t *testing.T) {
	isolate(t)
	_, err := runRoot(t, "auth", "switch", "ghost")
	if err == nil || !strings.Contains(err.Error(), "no stored credentials") {
		t.Fatalf("err = %v, want a 'no stored credentials' error", err)
	}
}

func TestAuthLogout(t *testing.T) {
	isolate(t)
	seedProfile(t, "work")
	if err := auth.SwitchProfile("work"); err != nil { // make it active so logout has something to clear
		t.Fatal(err)
	}

	out, err := runRoot(t, "--profile", "work", "auth", "logout")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `Logged out profile "work"`) {
		t.Fatalf("logout output %q", out)
	}
	if b := auth.NewStore().Backend("work"); b != "none" {
		t.Fatalf("backend after logout = %q, want none", b)
	}
	if s, _ := auth.LoadState(); s.Active == "work" {
		t.Fatal("logout should clear the active pointer")
	}
}
