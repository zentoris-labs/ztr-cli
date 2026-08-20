package auth

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestStoreKeychainRoundTrip(t *testing.T) {
	keyring.MockInit() // in-memory keychain; no real OS backend is touched
	s := &Store{dir: t.TempDir()}

	if err := s.Save("default", &Credentials{AccessToken: "at", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	if b := s.Backend("default"); b != "keychain" {
		t.Fatalf("backend = %q, want keychain", b)
	}
	// A keychain save must not leave a plaintext file behind.
	if _, err := os.Stat(s.path("default")); !os.IsNotExist(err) {
		t.Fatal("expected no fallback file when the keychain is used")
	}

	got, err := s.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AccessToken != "at" || got.RefreshToken != "rt" {
		t.Fatalf("loaded %+v, want at/rt", got)
	}

	if err := s.Clear("default"); err != nil {
		t.Fatal(err)
	}
	if b := s.Backend("default"); b != "none" {
		t.Fatalf("backend after clear = %q, want none", b)
	}
	got, err = s.Load("default")
	if err != nil || got != nil {
		t.Fatalf("after clear: got %+v, err %v; want (nil, nil)", got, err)
	}
}

func TestStoreFileFallbackWhenNoKeychain(t *testing.T) {
	keyring.MockInitWithError(keyring.ErrUnsupportedPlatform) // simulate a headless box: no backend
	s := &Store{dir: t.TempDir()}

	if err := s.Save("ci", &Credentials{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.path("ci"))
	if err != nil {
		t.Fatalf("expected a fallback file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("fallback file mode = %v, want 0600", perm)
	}
	if b := s.Backend("ci"); b != "file" {
		t.Fatalf("backend = %q, want file", b)
	}

	got, err := s.Load("ci")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AccessToken != "at" {
		t.Fatalf("loaded %+v, want at", got)
	}

	if err := s.Clear("ci"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path("ci")); !os.IsNotExist(err) {
		t.Fatal("expected the fallback file to be removed on clear")
	}
}
