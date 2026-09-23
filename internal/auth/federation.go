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
	"sync"
	"time"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// OIDCFederationSource is the CI credential path. Rather than integrating each CI vendor,
// it treats them uniformly: every major CI can mint a short-lived OIDC JWT, and Zentoris
// exchanges ANY trusted issuer's JWT for a scoped token (RFC 8693). The issuer, audience,
// and claim-match rules live in a trust configured on the server and named by ZENTORIS_TRUST_ID,
// so adding a new CI vendor is customer configuration, not zentoris code.
//
// The ONE place vendors differ is how the runner hands us the JWT, so that is the only
// pluggable part here: a short list of token providers, tried in order. Most CIs expose the
// JWT as an env var or file, which the generic provider covers with zero vendor code; only
// a couple (e.g. GitHub Actions) require an API call, handled by a tiny fetcher.
//
// The exchanged token is cached in memory until shortly before it expires, so a command that
// makes several API calls exchanges once; nothing is written to disk (a CI credential is
// short-lived by design and the runner is thrown away).
type OIDCFederationSource struct {
	cfg       *config.Config
	providers []oidcTokenProvider

	mu     sync.Mutex
	cached string
	expiry time.Time
}

// RFC 8693 token-exchange identifiers: the grant, the type of the JWT we hand over, and the
// type of token we ask for in return.
const (
	tokenExchangeGrant   = "urn:ietf:params:oauth:grant-type:token-exchange"
	jwtTokenType         = "urn:ietf:params:oauth:token-type:jwt"
	accessTokenTokenType = "urn:ietf:params:oauth:token-type:access_token"
)

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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != "" && time.Now().Before(s.expiry) {
		return s.cached, nil
	}

	jwt, provider, err := s.fetchOIDCToken(ctx)
	if err != nil {
		return "", err
	}
	if jwt == "" {
		return "", ErrNoCredential // no CI OIDC token available in this environment
	}
	// A CI identity IS present, so a missing trust id is a misconfiguration, not an absent
	// credential: say so here rather than letting the chain report "no credential found" or the
	// server answer with its deliberately uninformative uniform reject.
	if strings.TrimSpace(s.cfg.TrustID) == "" {
		return "", fmt.Errorf("a %s OIDC token is available but ZENTORIS_TRUST_ID is not set; "+
			"set it to the id of the federated trust this job exchanges under", provider)
	}

	token, ttl, err := s.exchange(ctx, jwt)
	if err != nil {
		return "", fmt.Errorf("exchanging the %s OIDC token: %w", provider, err)
	}
	s.cached = token
	s.expiry = time.Now().Add(ttl - 30*time.Second) // refresh a little early
	return token, nil
}

// exchange trades the vendor-agnostic JWT for a Zentoris token (RFC 8693). Zentoris validates
// the issuer's JWKS and audience, then matches the JWT claims (repository, branch, workflow, ...)
// against the conditions of the named trust (ZENTORIS_TRUST_ID), and returns a short-lived token
// acting as that trust's service account. No secret is involved: the trust is in the claims.
func (s *OIDCFederationSource) exchange(ctx context.Context, jwt string) (token string, ttl time.Duration, err error) {
	endpoint := fmt.Sprintf("%s/tenants/%s/oauth2/token",
		strings.TrimRight(s.cfg.AuthBase, "/"), opTenant)
	form := url.Values{
		"grant_type":           {tokenExchangeGrant},
		"subject_token":        {jwt},
		"subject_token_type":   {jwtTokenType},
		"requested_token_type": {accessTokenTokenType},
		// trust_id names the exact trust to assume (issuer + audience + conditions + the service
		// account it acts as). An identifier, not a credential: the signed subject_token and the
		// trust's conditions authorize. No client_id - this grant is anonymous by design.
		"trust_id": {strings.TrimSpace(s.cfg.TrustID)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.cfg.HTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", 0, s.exchangeError(resp.Status, data)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", 0, fmt.Errorf("decode token response: %w", err)
	}
	if body.AccessToken == "" {
		return "", 0, fmt.Errorf("token endpoint returned no access_token")
	}
	ttl = time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return body.AccessToken, ttl, nil
}

// exchangeError renders a failed exchange. A rejected exchange answers with a uniform
// invalid_grant carrying no reason - deliberately, so that a caller cannot probe a trust one
// condition at a time - which leaves the operator with nothing to act on. For that one code, name
// what has to line up; anything else already says enough on its own.
func (s *OIDCFederationSource) exchangeError(status string, body []byte) error {
	err := tokenEndpointError(status, body)
	var p struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &p)
	if p.Error != "invalid_grant" {
		return err
	}
	return fmt.Errorf("%w - the exchange was rejected without a reason, which is expected: check that "+
		"trust %q exists and is enabled, that its issuer and audience match the token this runner minted, "+
		"and that the token's claims satisfy the trust's conditions",
		err, strings.TrimSpace(s.cfg.TrustID))
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
