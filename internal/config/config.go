// Package config carries zentoris's resolved settings: the API endpoints, the active profile, TLS
// behavior, and the raw credential inputs. Values come from defaults, then environment
// variables, then persistent flags (flags win). Fields are read lazily by the auth sources and API
// client, so a flag parsed after construction still takes effect.
package config

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// defaultDomain is the only hardcoded host: the hosted Zentoris platform every user reaches by
// default. Any other deployment is selected by passing its full base domain (see Domain), so no
// non-public environment names live in this repository.
const defaultDomain = "zentoris.com"

// Config is the single settings struct threaded through the command tree.
type Config struct {
	// APIBase / AuthBase are the resolved service base URLs the API client and auth sources read.
	// They are derived from Domain (see below), never set directly - Domain is the single endpoint
	// knob, so there is no split between the two hosts to keep in sync.
	APIBase  string // Zentoris main API base URL (derived: main.api.<domain>)
	AuthBase string // Zentoris auth / OP base URL (derived: auth.api.<domain>)
	Profile  string // profile a command runs as: --profile > ZENTORIS_PROFILE > active (>= "default")
	// ProfileFromFlag is true when the profile came from the --profile flag (a one-off override), as
	// opposed to ZENTORIS_PROFILE or the active profile. Used only to decide whether `auth login`
	// prints the "not active" hint - so the auth layer needn't re-read the environment.
	ProfileFromFlag bool
	Insecure        bool // skip TLS verification (self-signed local dev)

	// Domain is the base host the service URLs are derived from as <svc>.api.<domain>. It defaults
	// to the hosted platform; pass another base domain (--domain / ZENTORIS_DOMAIN) to reach a
	// different deployment. Domain is the ONLY knob that selects a deployment: the CLI's OAuth
	// client id, login scope, and tenant are fixed first-party constants in internal/auth (this is
	// Zentoris's own first-party tooling, like the console's fixed `console` client), not settings.
	Domain string

	// Credential inputs, empty means "not provided".
	Token        string // raw bearer or apt_ PAT: --token / ZENTORIS_TOKEN
	ClientID     string // ZENTORIS_CLIENT_ID
	ClientSecret string // ZENTORIS_CLIENT_SECRET
}

// Load builds a Config from a small set of environment variables and built-in defaults. Only the
// things worth setting once per shell or injecting in CI get an env var: credentials, the profile,
// and the endpoint (ZENTORIS_DOMAIN). TLS-skip is flag-only. Base URLs derive from the domain
// (default the hosted platform).
func Load() *Config {
	c := &Config{
		Domain: envOr("ZENTORIS_DOMAIN", defaultDomain),
		// Profile is left as the raw ZENTORIS_PROFILE (possibly empty) here; the command layer
		// resolves the final value after flags parse (flag > env > active profile), since the
		// persisted active profile lives in internal/auth, which config must not import.
		Profile:      os.Getenv("ZENTORIS_PROFILE"),
		Token:        os.Getenv("ZENTORIS_TOKEN"),
		ClientID:     os.Getenv("ZENTORIS_CLIENT_ID"),
		ClientSecret: os.Getenv("ZENTORIS_CLIENT_SECRET"),
	}
	// deriveFromDomain is the single place APIBase/AuthBase/Insecure are computed; ApplyDomain
	// (the CLI path) validates first and reruns it, but a bare Load() must still be usable.
	c.deriveFromDomain(false)
	return c
}

// ApplyDomain validates c.Domain and re-derives the service base URLs (and the self-signed-dev
// TLS default) from it. It runs after flag parsing so a --domain flag composes correctly with the
// environment defaults resolved in Load: an explicit --insecure always wins over the derived TLS
// default. Returns an error for a malformed domain.
func (c *Config) ApplyDomain(explicitInsecure bool) error {
	if err := validateDomain(c.Domain); err != nil {
		return err
	}
	c.deriveFromDomain(explicitInsecure)
	return nil
}

// deriveFromDomain sets APIBase / AuthBase / Insecure from c.Domain. The base URLs are always
// derived; only Insecure yields to an explicitly-set --insecure flag.
func (c *Config) deriveFromDomain(explicitInsecure bool) {
	c.APIBase, c.AuthBase = endpointsFor(c.Domain)
	if !explicitInsecure {
		c.Insecure = isLocalDomain(c.Domain)
	}
}

// validateDomain rejects a --domain / ZENTORIS_DOMAIN value that is not a bare host, so the error
// is reported at config time instead of producing a silently malformed URL later.
func validateDomain(domain string) error {
	d := strings.TrimSpace(domain)
	if d == "" {
		return fmt.Errorf("domain must not be empty")
	}
	if strings.ContainsAny(d, "/: ") {
		return fmt.Errorf("invalid domain %q: pass a bare host like %s, not a URL or host:port", domain, defaultDomain)
	}
	return nil
}

// endpointsFor derives the main + auth API base URLs from a base domain (<svc>.api.<domain>).
func endpointsFor(domain string) (apiBase, authBase string) {
	d := strings.TrimSpace(strings.ToLower(domain))
	if d == "" {
		d = defaultDomain
	}
	return "https://main.api." + d, "https://auth.api." + d
}

// isLocalDomain reports whether a base domain is a self-signed local-dev host, for which TLS
// verification is skipped by default. It requires the trusted defaultDomain suffix so that an
// arbitrary host merely NAMED local (e.g. local.attacker.com) does not silently disable TLS.
func isLocalDomain(domain string) bool {
	d := strings.TrimSpace(strings.ToLower(domain))
	suffix := "." + defaultDomain
	if !strings.HasSuffix(d, suffix) {
		return false
	}
	label := strings.TrimSuffix(d, suffix)
	return label == "local" || strings.HasPrefix(label, "local-")
}

// HTTPClient builds an HTTP client honoring the Insecure flag. InsecureSkipVerify is opt-in
// only, for a self-signed local stack, and is never a default against a real deployment.
func (c *Config) HTTPClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{}
	if c.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
