package auth

import (
	"encoding/base64"
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
	if err := s.Save(); err != nil {
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
	if ActiveProfile() != "work" {
		t.Errorf("active = %q, want work", ActiveProfile())
	}
}

func TestRegisterAndUnregisterLogin(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	if err := RegisterLogin("work"); err != nil {
		t.Fatal(err)
	}
	if ActiveProfile() != "work" {
		t.Errorf("a fresh login should become active; got %q", ActiveProfile())
	}
	if err := UnregisterLogout("work"); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadState()
	if s.has("work") || s.Active == "work" {
		t.Errorf("logout should drop the profile and clear active; got %+v", s)
	}
}

func TestListProfiles(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	if err := NewStore().Save("work", &Credentials{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterLogin("work"); err != nil {
		t.Fatal(err)
	}
	if err := NewStore().Save("personal", &Credentials{AccessToken: "at2"}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterLogin("personal"); err != nil { // personal becomes active (most recent login)
		t.Fatal(err)
	}

	infos, err := ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("got %d profiles, want 2: %+v", len(infos), infos)
	}
	// Sorted: personal, work. personal is active (logged in last).
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
