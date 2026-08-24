package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
)

// Credentials is what `zentoris auth login` caches per account. The secret material (access + refresh
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

// Store persists per-account credentials. It prefers the OS keychain (macOS Keychain, Windows
// Credential Manager, Linux Secret Service via zalando/go-keyring) and falls back to a 0600 file
// under the user config dir on headless boxes where no keychain backend is reachable - the gh
// model. One source of truth per account: a successful keychain write removes any stale file.
type Store struct{ dir string }

// zentorisDir resolves the per-user directory for zentoris state: the credential-file fallback and
// the account index (config.json). It is ~/.zentoris (a dotfolder in the home directory, like
// ~/.aws or ~/.kube). A package var so tests can point it at a temp dir.
var zentorisDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "." // last resort: a .zentoris dir under the working directory
	}
	return filepath.Join(home, ".zentoris")
}

// NewStore locates the file-fallback directory (XDG config dir, falling back to home).
func NewStore() *Store { return &Store{dir: zentorisDir()} }

func normAccount(account string) string {
	if account == "" {
		return "default"
	}
	return account
}

func keyringUser(account string) string { return "credentials-" + normAccount(account) }

func (s *Store) path(account string) string {
	return filepath.Join(s.dir, "credentials-"+normAccount(account)+".json")
}

// Load returns the stored credentials for an account, or (nil, nil) when none exist. Reads the
// keychain first, then the file - so creds written on a headless box (or by an older file-only
// build) are still found, and a keychain that is present but empty falls through to the file.
func (s *Store) Load(account string) (*Credentials, error) {
	if v, err := keyring.Get(keyringService, keyringUser(account)); err == nil {
		var c Credentials
		if err := json.Unmarshal([]byte(v), &c); err != nil {
			return nil, err
		}
		return &c, nil
	}
	// Keychain miss (ErrNotFound) or unavailable backend both fall through to the file fallback.
	b, err := os.ReadFile(s.path(account))
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

// Save writes credentials for an account. Prefers the keychain; on any keychain error (no backend,
// as on a headless Linux host) it writes a 0600 file instead. A keychain success clears any stale
// file so the two backends never disagree.
func (s *Store) Save(account string, c *Credentials) error {
	blob, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := keyring.Set(keyringService, keyringUser(account), string(blob)); err == nil {
		_ = os.Remove(s.path(account)) // one source of truth: drop the file the fallback may have left
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	pretty, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(account), pretty, 0o600)
}

// Clear removes an account's stored credentials from BOTH backends; a missing entry is not an error.
func (s *Store) Clear(account string) error {
	if err := keyring.Delete(keyringService, keyringUser(account)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		// A missing entry or an unavailable keychain is fine; only a real delete failure matters, and
		// even then we still try to remove the file so logout is not left half-done.
		_ = err
	}
	if err := os.Remove(s.path(account)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Backend reports where an account's credentials currently live: "keychain", "file", or "none".
// Used by `zentoris auth status` so the user knows whether their token sits in the OS keychain or a file.
func (s *Store) Backend(account string) string {
	if _, err := keyring.Get(keyringService, keyringUser(account)); err == nil {
		return "keychain"
	}
	if _, err := os.Stat(s.path(account)); err == nil {
		return "file"
	}
	return "none"
}
