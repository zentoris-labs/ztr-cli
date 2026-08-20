package auth

import (
	"context"
	"strings"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// RawTokenSource passes an explicit bearer straight through: --token, ZENTORIS_TOKEN, or a
// personal access token (apt_...). Zentoris routes apt_ tokens to the API-token handler and
// everything else to JWT validation, so zentoris need not know which kind it holds.
type RawTokenSource struct{ cfg *config.Config }

// NewRawTokenSource reads the raw credential lazily from cfg so a --token flag parsed after
// construction is still honored.
func NewRawTokenSource(cfg *config.Config) *RawTokenSource { return &RawTokenSource{cfg: cfg} }

func (s *RawTokenSource) Name() string { return "token" }

func (s *RawTokenSource) Token(context.Context) (string, error) {
	raw := strings.TrimSpace(s.cfg.Token)
	if raw == "" {
		return "", ErrNoCredential
	}
	return raw, nil
}
