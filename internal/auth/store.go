package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
)

// Credentials is what `zentoris auth login` caches per profile. The secret material (access + refresh
// tokens) is stored in the OS keychain when one is available, and in a 0600 file otherwise - see
// Store. The struct itself is the JSON payload for both backends.
type Credentials struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	Subject      string    `json:"subject,omitempty"`
}

// keyringService is the OS-keychain service name every zentoris credential is filed under (the label a
// user sees in Keychain Access / Credential Manager, so it names the tool, not the OAuth client).
const keyringService = "zentoris"

// Store persists per-profile credentials. It prefers the OS keychain (macOS Keychain, Windows
// Credential Manager, Linux Secret Service via zalando/go-keyring) and falls back to a 0600 file
// under the user config dir on headless boxes where no keychain backend is reachable - the gh
// model. One source of truth per profile: a successful keychain write removes any stale file.
type Store struct{ dir string }

// NewStore locates the file-fallback directory (XDG config dir, falling back to home).
func NewStore() *Store {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = home
	}
	return &Store{dir: filepath.Join(base, "zentoris")}
}

func normProfile(profile string) string {
	if profile == "" {
		return "default"
	}
	return profile
}

func keyringUser(profile string) string { return "credentials-" + normProfile(profile) }

func (s *Store) path(profile string) string {
	return filepath.Join(s.dir, "credentials-"+normProfile(profile)+".json")
}

// Load returns the stored credentials for a profile, or (nil, nil) when none exist. Reads the
// keychain first, then the file - so creds written on a headless box (or by an older file-only
// build) are still found, and a keychain that is present but empty falls through to the file.
func (s *Store) Load(profile string) (*Credentials, error) {
	if v, err := keyring.Get(keyringService, keyringUser(profile)); err == nil {
		var c Credentials
		if err := json.Unmarshal([]byte(v), &c); err != nil {
			return nil, err
		}
		return &c, nil
	}
	// Keychain miss (ErrNotFound) or unavailable backend both fall through to the file fallback.
	b, err := os.ReadFile(s.path(profile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes credentials for a profile. Prefers the keychain; on any keychain error (no backend,
// as on a headless Linux host) it writes a 0600 file instead. A keychain success clears any stale
// file so the two backends never disagree.
func (s *Store) Save(profile string, c *Credentials) error {
	blob, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := keyring.Set(keyringService, keyringUser(profile), string(blob)); err == nil {
		_ = os.Remove(s.path(profile)) // one source of truth: drop the file the fallback may have left
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	pretty, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(profile), pretty, 0o600)
}

// Clear removes a profile's stored credentials from BOTH backends; a missing entry is not an error.
func (s *Store) Clear(profile string) error {
	if err := keyring.Delete(keyringService, keyringUser(profile)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		// A missing entry or an unavailable keychain is fine; only a real delete failure matters, and
		// even then we still try to remove the file so logout is not left half-done.
		_ = err
	}
	if err := os.Remove(s.path(profile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Backend reports where a profile's credentials currently live: "keychain", "file", or "none".
// Used by `zentoris auth status` so the user knows whether their token sits in the OS keychain or a file.
func (s *Store) Backend(profile string) string {
	if _, err := keyring.Get(keyringService, keyringUser(profile)); err == nil {
		return "keychain"
	}
	if _, err := os.Stat(s.path(profile)); err == nil {
		return "file"
	}
	return "none"
}
