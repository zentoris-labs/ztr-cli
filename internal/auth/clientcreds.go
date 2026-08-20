package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// ClientCredentialsSource performs the OAuth2 client_credentials grant against the Zentoris
// OP token endpoint using ZENTORIS_CLIENT_ID / ZENTORIS_CLIENT_SECRET, caching the token in memory
// until shortly before it expires. This is the static machine-to-machine path; the CI
// destination is FederationSource (no stored secret).
type ClientCredentialsSource struct {
	cfg *config.Config

	mu     sync.Mutex
	cached string
	expiry time.Time
}

// NewClientCredentialsSource builds the source; it yields ErrNoCredential unless both the
// client id and secret are configured.
func NewClientCredentialsSource(cfg *config.Config) *ClientCredentialsSource {
	return &ClientCredentialsSource{cfg: cfg}
}

func (s *ClientCredentialsSource) Name() string { return "client-credentials" }

func (s *ClientCredentialsSource) Token(ctx context.Context) (string, error) {
	if s.cfg.ClientID == "" || s.cfg.ClientSecret == "" {
		return "", ErrNoCredential
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != "" && time.Now().Before(s.expiry) {
		return s.cached, nil
	}

	endpoint := fmt.Sprintf("%s/tenants/%s/oauth2/token",
		strings.TrimRight(s.cfg.AuthBase, "/"), url.PathEscape(s.cfg.Tenant))
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {s.cfg.ClientID},
		"client_secret": {s.cfg.ClientSecret},
	}
	if s.cfg.Resource != "" {
		form.Set("resource", s.cfg.Resource) // RFC 8707: the OP requires it to pick the audience
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.cfg.HTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return "", fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", tokenEndpointError(resp.Status, data)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("token endpoint returned no access_token")
	}

	ttl := time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	s.cached = body.AccessToken
	s.expiry = time.Now().Add(ttl - 30*time.Second) // refresh a little early
	return s.cached, nil
}

// tokenEndpointError renders an OP token-endpoint failure body (an RFC 6749 OAuth error or an
// RFC 9457 problem+json) into a readable message. Error bodies carry no token, so this is safe.
func tokenEndpointError(status string, body []byte) error {
	var p struct {
		Error     string `json:"error"`
		ErrorDesc string `json:"error_description"`
		Title     string `json:"title"`
		Detail    string `json:"detail"`
	}
	_ = json.Unmarshal(body, &p)
	switch {
	case p.ErrorDesc != "":
		return fmt.Errorf("token endpoint %s: %s (%s)", status, p.ErrorDesc, p.Error)
	case p.Detail != "":
		return fmt.Errorf("token endpoint %s: %s", status, p.Detail)
	case p.Error != "":
		return fmt.Errorf("token endpoint %s: %s", status, p.Error)
	default:
		return fmt.Errorf("token endpoint returned %s", status)
	}
}
