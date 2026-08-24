package auth

import (
	"encoding/base64"
	"os"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

// withTempDir points the state directory and credential store at a fresh temp dir for one test.
func withTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old := zentorisDir
	zentorisDir = func() string { return dir }
	t.Cleanup(func() { zentorisDir = old })
}

// mustActive returns ActiveProfile(), failing the test on a read error.
func mustActive(t *testing.T) string {
	t.Helper()
	a, err := ActiveProfile()
	if err != nil {
		t.Fatalf("ActiveProfile: %v", err)
	}
	return a
}

func TestStateRoundTrip(t *testing.T) {
	withTempDir(t)

	// A missing state file loads as empty, not an error.
	s, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState on empty dir: %v", err)
	}
	if s.Active != "" || len(s.Profiles) != 0 {
		t.Fatalf("empty state expected, got %+v", s)
	}

	s.add("work")
	s.add("work") // idempotent
	s.add("personal")
	s.Active = "work"
	if err := s.save(); err != nil {
		t.Fatal(err)
	}

	got, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if got.Active != "work" {
		t.Errorf("active = %q, want work", got.Active)
	}
	if len(got.Profiles) != 2 || got.Profiles[0] != "personal" || got.Profiles[1] != "work" {
		t.Errorf("profiles = %v, want sorted [personal work]", got.Profiles)
	}

	got.remove("work")
	if got.has("work") {
		t.Error("work should be gone after remove")
	}
	if got.Active != "" {
		t.Error("removing the active profile should clear Active")
	}
}

func TestConcurrentRegisterProfileKeepsAll(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	names := []string{"a", "b", "c", "d", "e", "f"}
	var wg sync.WaitGroup
	for _, n := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			if err := RegisterProfile(n); err != nil {
				t.Errorf("RegisterProfile(%s): %v", n, err)
			}
		}(n)
	}
	wg.Wait()

	// Without the shared state lock, concurrent LoadState->add->Save cycles clobber each other and
	// some names are lost; with it, every profile survives in the index.
	s, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if !s.has(n) {
			t.Errorf("profile %q lost from the index under concurrent registration", n)
		}
	}
}

func TestActiveProfileErrorsOnCorruptState(t *testing.T) {
	withTempDir(t)
	// A valid-but-empty state yields "default" with no error.
	if a, err := ActiveProfile(); err != nil || a != "default" {
		t.Fatalf("empty state: got (%q, %v), want (default, nil)", a, err)
	}
	// A present-but-unparseable state file must surface an error, NOT silently resolve to "default"
	// (which would run commands as the wrong identity).
	if err := os.MkdirAll(zentorisDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ActiveProfile(); err == nil {
		t.Fatal("ActiveProfile must return an error for a corrupt state file")
	}
}

func TestSwitchProfileRequiresCredentials(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	// Switching to a profile with no stored credentials is refused.
	if err := SwitchProfile("ghost"); err == nil {
		t.Fatal("expected an error switching to a profile with no credentials")
	}

	if err := NewStore().Save("work", &Credentials{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	if err := SwitchProfile("work"); err != nil {
		t.Fatalf("switch to a logged-in profile: %v", err)
	}
	if mustActive(t) != "work" {
		t.Errorf("active = %q, want work", mustActive(t))
	}
}

func TestRegisterAndUnregisterProfile(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	if err := RegisterProfile("work"); err != nil {
		t.Fatal(err)
	}
	// Registering a login is passive: it indexes the profile but must NOT activate it.
	if mustActive(t) != "default" {
		t.Errorf("RegisterProfile must not change the active profile (stays default); got %q", mustActive(t))
	}
	if s, _ := LoadState(); !s.has("work") {
		t.Error("RegisterProfile should add the profile to the index")
	}
	if err := UnregisterLogout("work"); err != nil {
		t.Fatal(err)
	}
	if s, _ := LoadState(); s.has("work") {
		t.Error("logout should drop the profile from the index")
	}
}

func TestListProfiles(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	for _, name := range []string{"work", "personal"} {
		if err := NewStore().Save(name, &Credentials{AccessToken: "at-" + name}); err != nil {
			t.Fatal(err)
		}
		if err := RegisterProfile(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := SwitchProfile("personal"); err != nil { // active is set only by an explicit switch
		t.Fatal(err)
	}

	infos, err := ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("got %d profiles, want 2: %+v", len(infos), infos)
	}
	// Sorted: personal, work. personal is the one we switched to.
	if infos[0].Name != "personal" || !infos[0].Active {
		t.Errorf("expected personal active and first, got %+v", infos[0])
	}
	if infos[1].Name != "work" || infos[1].Active {
		t.Errorf("expected work present and inactive, got %+v", infos[1])
	}
	if infos[0].Backend != "keychain" {
		t.Errorf("backend = %q, want keychain", infos[0].Backend)
	}
}

func TestSubjectFromToken(t *testing.T) {
	// A minimal JWT: header.payload.signature; only the payload's claims matter here.
	mkJWT := func(payloadJSON string) string {
		enc := base64.RawURLEncoding.EncodeToString
		return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payloadJSON)) + ".sig"
	}
	cases := []struct{ token, want string }{
		{mkJWT(`{"sub":"u-1","email":"a@b.test"}`), "a@b.test"},        // email preferred
		{mkJWT(`{"sub":"u-1","preferred_username":"alice"}`), "alice"}, // then username
		{mkJWT(`{"sub":"u-1"}`), "u-1"},                                // fall back to sub
		{"opaque-token", ""},                                           // not a JWT
		{"a.b", ""},                                                    // wrong segment count
		{mkJWT(`not json`), ""},                                        // unparseable payload
	}
	for _, c := range cases {
		if got := subjectFromToken(c.token); got != c.want {
			t.Errorf("subjectFromToken(%q) = %q, want %q", c.token, got, c.want)
		}
	}
}
