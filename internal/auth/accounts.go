package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// stateFile is the account index, stored as JSON next to the credential files.
const stateFile = "config.json"

// State is the persisted account index. OS keychains are not enumerable, so zentoris tracks which
// accounts have been logged in (Accounts) and which one is the active default (Active) itself -
// the model gh uses for its hosts file. `auth switch` sets Active; `auth list` reads Accounts.
type State struct {
	Active   string   `json:"activeAccount,omitempty"`
	Accounts []string `json:"accounts,omitempty"`
}

func statePath() string { return filepath.Join(zentorisDir(), stateFile) }

// LoadState reads the account index, returning an empty State when none exists yet.
func LoadState() (*State, error) {
	b, err := os.ReadFile(statePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes the account index as a 0600 file under the 0700 state directory.
func (s *State) Save() error {
	if err := os.MkdirAll(zentorisDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), b, 0o600)
}

func (s *State) has(account string) bool {
	for _, a := range s.Accounts {
		if a == account {
			return true
		}
	}
	return false
}

// add records an account in the index (idempotent), keeping the list sorted.
func (s *State) add(account string) {
	if account == "" || s.has(account) {
		return
	}
	s.Accounts = append(s.Accounts, account)
	sort.Strings(s.Accounts)
}

// remove drops an account from the index and clears Active if it pointed there.
func (s *State) remove(account string) {
	kept := make([]string, 0, len(s.Accounts))
	for _, a := range s.Accounts {
		if a != account {
			kept = append(kept, a)
		}
	}
	s.Accounts = kept
	if s.Active == account {
		s.Active = ""
	}
}

// ActiveAccount returns the account marked active by `auth switch`, or "" when none is set. Used to
// resolve the default account when neither --account nor ZENTORIS_ACCOUNT is given.
func ActiveAccount() string {
	s, err := LoadState()
	if err != nil {
		return ""
	}
	return s.Active
}

// RegisterLogin records a successful login under account and makes it the active default (a fresh
// login switches to that account, matching gh).
func RegisterLogin(account string) error {
	account = normAccount(account)
	s, err := LoadState()
	if err != nil {
		return err
	}
	s.add(account)
	s.Active = account
	return s.Save()
}

// UnregisterLogout removes account from the index after logout, clearing Active if it was active.
func UnregisterLogout(account string) error {
	account = normAccount(account)
	s, err := LoadState()
	if err != nil {
		return err
	}
	s.remove(account)
	return s.Save()
}

// SwitchAccount makes account the active default. It requires that account already has stored
// credentials, so you cannot switch to an account you have not logged into.
func SwitchAccount(account string) error {
	account = normAccount(account)
	if NewStore().Backend(account) == "none" {
		return fmt.Errorf("no stored credentials for account %q; run `zentoris --account %s auth login` first", account, account)
	}
	s, err := LoadState()
	if err != nil {
		return err
	}
	s.add(account)
	s.Active = account
	return s.Save()
}

// AccountInfo is one row of `zentoris auth list`.
type AccountInfo struct {
	Name    string
	Active  bool
	Backend string // "keychain" | "file" | "none"
	Subject string // identity captured at login, if any
	Expired bool
}

// ListAccounts returns every known account - the index unioned with any credential files on disk,
// so a stray login still shows up - each with where its credentials live, the identity captured at
// login if any, whether the stored session has expired, and whether it is the active default.
func ListAccounts() ([]AccountInfo, error) {
	s, err := LoadState()
	if err != nil {
		return nil, err
	}
	names := map[string]struct{}{}
	for _, a := range s.Accounts {
		names[a] = struct{}{}
	}
	if entries, err := filepath.Glob(filepath.Join(zentorisDir(), "credentials-*.json")); err == nil {
		for _, e := range entries {
			name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(e), "credentials-"), ".json")
			if name != "" {
				names[name] = struct{}{}
			}
		}
	}

	store := NewStore()
	out := make([]AccountInfo, 0, len(names))
	for name := range names {
		info := AccountInfo{Name: name, Active: name == s.Active, Backend: store.Backend(name)}
		if creds, err := store.Load(name); err == nil && creds != nil {
			info.Subject = creds.Subject
			info.Expired = !creds.Expiry.IsZero() && time.Now().After(creds.Expiry)
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// subjectFromToken best-effort extracts an identity from a JWT access token, preferring a
// human-readable claim (email, then preferred_username) over the opaque subject. It returns "" for
// an opaque or unparseable token, so a login is simply left unlabeled rather than failing.
func subjectFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	switch {
	case claims.Email != "":
		return claims.Email
	case claims.PreferredUsername != "":
		return claims.PreferredUsername
	default:
		return claims.Sub
	}
}
