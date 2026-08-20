// Package config carries zentoris's resolved settings: API endpoints, the active tenant and
// profile, the output format, TLS behavior, and the raw credential inputs. Values come from
// defaults, then environment variables, then persistent flags (flags win). Fields are read
// lazily by the auth sources and API client, so a flag parsed after construction still takes
// effect.
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
	APIBase  string // Zentoris main API base URL
	AuthBase string // Zentoris auth / OP base URL
	Tenant   string // tenant id used in OP token-endpoint paths
	Output   string // "table" | "json"
	Profile  string // named credential profile
	Insecure bool   // skip TLS verification (self-signed local dev)

	// Domain is the base host the service URLs are derived from as <svc>.api.<domain>. It defaults
	// to the hosted platform; pass another base domain to reach a different deployment. Explicit
	// APIBase / AuthBase always win over what Domain would derive.
	Domain string

	// NOTE: the CLI's OAuth client id and login scope are NOT configurable - they are fixed
	// constants in internal/auth (this is Zentoris's own first-party tooling, like the console's
	// fixed `console` client). Only the TENANT (whose data) varies, never the client (which tool).
	Resource string // ZENTORIS_RESOURCE: RFC 8707 resource indicator (the target API audience)

	// Credential inputs, empty means "not provided".
	Token        string // raw bearer or apt_ PAT: --token / ZENTORIS_TOKEN
	ClientID     string // ZENTORIS_CLIENT_ID
	ClientSecret string // ZENTORIS_CLIENT_SECRET
}

// Load builds a Config from a small set of environment variables and built-in defaults. Only the
// things worth setting once per shell or injecting in CI get an env var: credentials, the profile,
// and the endpoint (ZENTORIS_DOMAIN). Preferences like tenant, output, TLS-skip, and the resource
// audience are flag-only. Base URLs derive from the domain (default the hosted platform).
func Load() *Config {
	c := &Config{
		Domain:       envOr("ZENTORIS_DOMAIN", defaultDomain),
		Tenant:       "main",
		Output:       "table",
		Profile:      envOr("ZENTORIS_PROFILE", "default"),
		Token:        os.Getenv("ZENTORIS_TOKEN"),
		ClientID:     os.Getenv("ZENTORIS_CLIENT_ID"),
		ClientSecret: os.Getenv("ZENTORIS_CLIENT_SECRET"),
	}
	// deriveFromDomain is the single place APIBase/AuthBase/Insecure are computed; ApplyDomain
	// (the CLI path) validates first and reruns it, but a bare Load() must still be usable.
	c.deriveFromDomain(false, false, false)
	return c
}

// ApplyDomain validates c.Domain and re-derives the service base URLs (and the self-signed-dev
// TLS default) from it, skipping any value the operator pinned directly. It runs after flag
// parsing so a --domain flag composes correctly with the environment defaults resolved in Load:
// explicit --api / --auth-url / --insecure always win. Returns an error for a malformed domain.
func (c *Config) ApplyDomain(explicitAPI, explicitAuth, explicitInsecure bool) error {
	if err := validateDomain(c.Domain); err != nil {
		return err
	}
	c.deriveFromDomain(explicitAPI, explicitAuth, explicitInsecure)
	return nil
}

// deriveFromDomain sets APIBase / AuthBase / Insecure from c.Domain, leaving any field the caller
// marked as explicitly pinned untouched.
func (c *Config) deriveFromDomain(explicitAPI, explicitAuth, explicitInsecure bool) {
	apiBase, authBase := endpointsFor(c.Domain)
	if !explicitAPI {
		c.APIBase = apiBase
	}
	if !explicitAuth {
		c.AuthBase = authBase
	}
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
