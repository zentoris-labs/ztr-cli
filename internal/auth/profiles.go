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

// save writes the profile index as a 0600 file under the 0700 state directory. It writes to a temp
// file and renames, so a concurrent reader never observes a half-written file. It is unexported so
// the only write path is updateState (which holds the shared state lock) - no caller can persist
// the index without the lock.
func (s *State) save() error {
	if err := os.MkdirAll(zentorisDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, statePath()); err != nil {
		_ = os.Remove(tmp) // don't leave a stale temp behind
		return err
	}
	return nil
}

// updateState applies mutate to the profile index under the shared state lock and persists it, so
// concurrent index changes (login / switch / logout across processes) cannot clobber each other. It
// writes only when mutate reports a change, and a mutate that returns an error aborts without
// writing.
func updateState(mutate func(*State) (changed bool, err error)) error {
	return withStateLock(func() error {
		s, err := LoadState()
		if err != nil {
			return err
		}
		changed, err := mutate(s)
		if err != nil || !changed {
			return err
		}
		return s.save()
	})
}

func (s *State) has(profile string) bool {
	for _, p := range s.Profiles {
		if p == profile {
			return true
		}
	}
	return false
}

// add records a profile in the index (idempotent), keeping the list sorted. It reports whether the
// profile was actually added (false when it was already present).
func (s *State) add(profile string) bool {
	if profile == "" || s.has(profile) {
		return false
	}
	s.Profiles = append(s.Profiles, profile)
	sort.Strings(s.Profiles)
	return true
}

// remove drops a profile from the index and clears Active if it pointed there. It reports whether
// anything changed.
func (s *State) remove(profile string) bool {
	kept := make([]string, 0, len(s.Profiles))
	for _, p := range s.Profiles {
		if p != profile {
			kept = append(kept, p)
		}
	}
	changed := len(kept) != len(s.Profiles)
	s.Profiles = kept
	if s.Active == profile {
		s.Active = ""
		changed = true
	}
	return changed
}

// ActiveProfile returns the profile every command runs as when neither --profile nor
// ZENTORIS_PROFILE is given: the one last chosen by `auth switch` (or `auth login --activate`), or
// "default" when none has been. The active profile is always a concrete value, seeded to "default".
// The error is non-nil only when the state file exists but cannot be read/parsed - callers should
// surface it rather than silently fall back to "default", which would run as the wrong identity.
func ActiveProfile() (string, error) {
	s, err := LoadState()
	if err != nil {
		return "", err
	}
	return normProfile(s.Active), nil // "" (never switched) means "default"
}

// RegisterProfile records a profile in the index (so `auth list` shows it) without touching the
// active profile. `auth login` is passive: it stores credentials and registers the profile, but
// only `auth switch` (or `auth login --activate`) changes which profile is active.
func RegisterProfile(profile string) error {
	profile = normProfile(profile)
	return updateState(func(s *State) (bool, error) { return s.add(profile), nil })
}

// UnregisterLogout removes profile from the index after logout, clearing Active if it was active.
func UnregisterLogout(profile string) error {
	profile = normProfile(profile)
	return updateState(func(s *State) (bool, error) { return s.remove(profile), nil })
}

// SwitchProfile makes profile the active default. It requires that profile already has stored
// credentials, so you cannot switch to a profile you have not logged into. The credentials check
// runs under the state lock (inside updateState) so it is atomic with setting the active profile.
func SwitchProfile(profile string) error {
	profile = normProfile(profile)
	return updateState(func(s *State) (bool, error) {
		if NewStore().Backend(profile) == "none" {
			return false, fmt.Errorf("no stored credentials for profile %q; run `zentoris --profile %s auth login` first", profile, profile)
		}
		changed := s.add(profile)
		if s.Active != profile {
			s.Active = profile
			changed = true
		}
		return changed, nil
	})
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

	active := normProfile(s.Active) // the active profile is always concrete; "" means "default"
	store := NewStore()
	out := make([]ProfileInfo, 0, len(names))
	for name := range names {
		info := ProfileInfo{Name: name, Active: name == active, Backend: store.Backend(name)}
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
