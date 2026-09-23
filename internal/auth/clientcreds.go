package auth

import (
	"context"
	"net/url"
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

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {s.cfg.ClientID},
		"client_secret": {s.cfg.ClientSecret},
	}
	token, refreshAt, err := postTokenEndpoint(ctx, s.cfg, form)
	if err != nil {
		return "", err
	}
	s.cached, s.expiry = token, refreshAt
	return s.cached, nil
}
