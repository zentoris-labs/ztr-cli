// Package auth resolves the bearer credential zentoris sends to the Zentoris API.
//
// Zentoris will accept several kinds of caller credential, so zentoris models each as a
// TokenSource and resolves them in a fixed precedence chain (highest first):
//
//  1. token        explicit --token / ZENTORIS_TOKEN (also how a PAT, apt_..., is passed)
//  2. login        credentials cached by `zentoris auth login` (interactive human developer)
//  3. client-creds ZENTORIS_CLIENT_ID / ZENTORIS_CLIENT_SECRET (static machine-to-machine)
//  4. oidc         CI OIDC (GitHub, GitLab, Buildkite, ... any trusted issuer), exchanged for a Zentoris token
//
// Today 1, 2, and 3 are wired end-to-end; 4 acquires the CI JWT but its Zentoris
// exchange is stubbed (see federation.go).
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// ErrNoCredential means a source has nothing to offer; the resolver tries the next one.
var ErrNoCredential = errors.New("no credential available from this source")

// TokenSource yields a bearer credential for the Authorization header.
type TokenSource interface {
	Name() string
	Token(ctx context.Context) (string, error)
}

// Resolver walks a precedence chain and returns the first credential found.
type Resolver struct{ sources []TokenSource }

// NewResolver builds a resolver over the given sources, tried in order.
func NewResolver(sources ...TokenSource) *Resolver { return &Resolver{sources: sources} }

// Token returns the first available credential and the name of the source that provided it.
func (r *Resolver) Token(ctx context.Context) (token, source string, err error) {
	var tried []string
	for _, s := range r.sources {
		tok, terr := s.Token(ctx)
		if errors.Is(terr, ErrNoCredential) {
			tried = append(tried, s.Name())
			continue
		}
		if terr != nil {
			return "", s.Name(), fmt.Errorf("%s: %w", s.Name(), terr)
		}
		return tok, s.Name(), nil
	}
	return "", "", fmt.Errorf(
		"no Zentoris credential found (tried: %s); run `zentoris auth login`, pass --token/ZENTORIS_TOKEN, or set ZENTORIS_CLIENT_ID/ZENTORIS_CLIENT_SECRET",
		strings.Join(tried, ", "),
	)
}

// DefaultChain wires the standard precedence: raw token, stored login, client creds, CI federation.
func DefaultChain(cfg *config.Config) *Resolver {
	return NewResolver(
		NewRawTokenSource(cfg),
		NewLoginSource(cfg),
		NewClientCredentialsSource(cfg),
		NewOIDCFederationSource(cfg),
	)
}
