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

// stateFile is the profile index, stored as JSON next to the credential files.
const stateFile = "config.json"

// State is the persisted profile index. OS keychains are not enumerable, so zentoris tracks which
// profiles have been logged in (Profiles) and which one is the active default (Active) itself -
// the model gh uses for its hosts file. `auth switch` sets Active; `auth list` reads Profiles.
type State struct {
	Active   string   `json:"activeProfile,omitempty"`
	Profiles []string `json:"profiles,omitempty"`
}

func statePath() string { return filepath.Join(zentorisDir(), stateFile) }

// LoadState reads the profile index, returning an empty State when none exists yet.
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

// Save writes the profile index as a 0600 file under the 0700 state directory.
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

func (s *State) has(profile string) bool {
	for _, p := range s.Profiles {
		if p == profile {
			return true
		}
	}
	return false
}

// add records a profile in the index (idempotent), keeping the list sorted.
func (s *State) add(profile string) {
	if profile == "" || s.has(profile) {
		return
	}
	s.Profiles = append(s.Profiles, profile)
	sort.Strings(s.Profiles)
}

// remove drops a profile from the index and clears Active if it pointed there.
func (s *State) remove(profile string) {
	kept := make([]string, 0, len(s.Profiles))
	for _, p := range s.Profiles {
		if p != profile {
			kept = append(kept, p)
		}
	}
	s.Profiles = kept
	if s.Active == profile {
		s.Active = ""
	}
}

// ActiveProfile returns the profile marked active by `auth switch`, or "" when none is set. Used to
// resolve the default profile when neither --profile nor ZENTORIS_PROFILE is given.
func ActiveProfile() string {
	s, err := LoadState()
	if err != nil {
		return ""
	}
	return s.Active
}

// RegisterLogin records a successful login under profile and makes it the active default (a fresh
// login switches to that profile, matching gh).
func RegisterLogin(profile string) error {
	profile = normProfile(profile)
	s, err := LoadState()
	if err != nil {
		return err
	}
	s.add(profile)
	s.Active = profile
	return s.Save()
}

// UnregisterLogout removes profile from the index after logout, clearing Active if it was active.
func UnregisterLogout(profile string) error {
	profile = normProfile(profile)
	s, err := LoadState()
	if err != nil {
		return err
	}
	s.remove(profile)
	return s.Save()
}

// SwitchProfile makes profile the active default. It requires that profile already has stored
// credentials, so you cannot switch to a profile you have not logged into.
func SwitchProfile(profile string) error {
	profile = normProfile(profile)
	if NewStore().Backend(profile) == "none" {
		return fmt.Errorf("no stored credentials for profile %q; run `zentoris --profile %s auth login` first", profile, profile)
	}
	s, err := LoadState()
	if err != nil {
		return err
	}
	s.add(profile)
	s.Active = profile
	return s.Save()
}

// ProfileInfo is one row of `zentoris auth list`.
type ProfileInfo struct {
	Name    string
	Active  bool
	Backend string // "keychain" | "file" | "none"
	Subject string // the account identity captured at login, if any
	Expired bool
}

// ListProfiles returns every known profile - the index unioned with any credential files on disk,
// so a stray login still shows up - each with where its credentials live, the account identity
// captured at login if any, whether the stored session has expired, and whether it is active.
func ListProfiles() ([]ProfileInfo, error) {
	s, err := LoadState()
	if err != nil {
		return nil, err
	}
	names := map[string]struct{}{}
	for _, p := range s.Profiles {
		names[p] = struct{}{}
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
	out := make([]ProfileInfo, 0, len(names))
	for name := range names {
		info := ProfileInfo{Name: name, Active: name == s.Active, Backend: store.Backend(name)}
		if creds, err := store.Load(name); err == nil && creds != nil {
			info.Subject = creds.Subject
			info.Expired = !creds.Expiry.IsZero() && time.Now().After(creds.Expiry)
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// subjectFromToken best-effort extracts the account identity from a JWT access token, preferring a
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
