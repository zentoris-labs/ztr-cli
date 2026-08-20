package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// OIDCFederationSource is the CI credential path. Rather than integrating each CI vendor,
// it treats them uniformly: every major CI can mint a short-lived OIDC JWT, and Zentoris
// exchanges ANY trusted issuer's JWT for a scoped token (RFC 8693). The issuer, audience,
// and claim-match rules live in a per-service trust policy on the server, so adding a new
// CI vendor is customer configuration, not zentoris code.
//
// The ONE place vendors differ is how the runner hands us the JWT, so that is the only
// pluggable part here: a short list of token providers, tried in order. Most CIs expose the
// JWT as an env var or file, which the generic provider covers with zero vendor code; only
// a couple (e.g. GitHub Actions) require an API call, handled by a tiny fetcher.
//
// STUB at the exchange step: the Zentoris token-exchange endpoint is not built yet.
type OIDCFederationSource struct {
	cfg       *config.Config
	providers []oidcTokenProvider
}

// NewOIDCFederationSource wires the token-provider registry: generic env/file first (covers
// GitLab, CircleCI, Buildkite-env, or a hand-provided token), then vendor fetchers.
func NewOIDCFederationSource(cfg *config.Config) *OIDCFederationSource {
	return &OIDCFederationSource{
		cfg: cfg,
		providers: []oidcTokenProvider{
			explicitOIDCToken{}, // ZENTORIS_OIDC_TOKEN / ZENTORIS_OIDC_TOKEN_FILE
			githubActionsOIDC{}, // ACTIONS_ID_TOKEN_REQUEST_URL
			// TODO: buildkiteAgentOIDC{} shells out to `buildkite-agent oidc request-token`, etc.
		},
	}
}

func (s *OIDCFederationSource) Name() string { return "oidc-federation" }

func (s *OIDCFederationSource) Token(ctx context.Context) (string, error) {
	jwt, provider, err := s.fetchOIDCToken(ctx)
	if err != nil {
		return "", err
	}
	if jwt == "" {
		return "", ErrNoCredential // no CI OIDC token available in this environment
	}
	// TODO(auth-federation): exchange the vendor-agnostic JWT at Zentoris:
	//   POST {AuthBase}/tenants/{tenant}/oauth2/token
	//     grant_type=urn:ietf:params:oauth:grant-type:token-exchange
	//     subject_token=<jwt>  subject_token_type=urn:ietf:params:oauth:token-type:jwt
	//   Zentoris validates the issuer's JWKS + audience and matches the JWT claims against the
	//   target service's trust policy, returning a short-lived per-service token.
	return "", fmt.Errorf("OIDC federation token acquired via %q, but the Zentoris token-exchange endpoint is not built yet; "+
		"use client credentials (ZENTORIS_CLIENT_ID/ZENTORIS_CLIENT_SECRET) as the bridge", provider)
}

// fetchOIDCToken returns the first OIDC JWT any provider can supply, and the provider name.
func (s *OIDCFederationSource) fetchOIDCToken(ctx context.Context) (jwt, provider string, err error) {
	audience := strings.TrimRight(s.cfg.AuthBase, "/")
	for _, p := range s.providers {
		tok, err := p.fetch(ctx, audience)
		if err != nil {
			return "", p.name(), err
		}
		if tok != "" {
			return tok, p.name(), nil
		}
	}
	return "", "", nil
}

// oidcTokenProvider knows how to obtain a raw OIDC JWT from one kind of CI runner. The
// exchange with Zentoris is identical regardless of which provider produced the JWT.
type oidcTokenProvider interface {
	name() string
	// fetch returns the JWT, or "" if this provider is not active in the current environment.
	fetch(ctx context.Context, audience string) (string, error)
}

// explicitOIDCToken covers every CI that already exposes its OIDC JWT as an env var or file
// (GitLab id_tokens, CircleCI $CIRCLE_OIDC_TOKEN, Buildkite env, or a hand-provided token).
type explicitOIDCToken struct{}

func (explicitOIDCToken) name() string { return "explicit" }

func (explicitOIDCToken) fetch(context.Context, string) (string, error) {
	if tok := strings.TrimSpace(os.Getenv("ZENTORIS_OIDC_TOKEN")); tok != "" {
		return tok, nil
	}
	if path := strings.TrimSpace(os.Getenv("ZENTORIS_OIDC_TOKEN_FILE")); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read ZENTORIS_OIDC_TOKEN_FILE: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", nil
}

// githubActionsOIDC fetches the JWT from the Actions token service - the common CI that
// requires an API call rather than exposing an env var.
type githubActionsOIDC struct{}

func (githubActionsOIDC) name() string { return "github-actions" }

func (githubActionsOIDC) fetch(ctx context.Context, audience string) (string, error) {
	reqURL := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	reqToken := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if reqURL == "" || reqToken == "" {
		return "", nil // not a GitHub Actions job with id-token: write
	}
	u := reqURL
	if audience != "" {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + "audience=" + url.QueryEscape(audience)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+reqToken)
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github actions token service returned %s", resp.Status)
	}
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode github oidc response: %w", err)
	}
	return parsed.Value, nil
}
