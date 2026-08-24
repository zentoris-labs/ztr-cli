package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

func clearOIDCEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"ZENTORIS_OIDC_TOKEN", "ZENTORIS_OIDC_TOKEN_FILE",
		"ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN",
	} {
		t.Setenv(k, "")
	}
}

func TestOIDCFederationName(t *testing.T) {
	if NewOIDCFederationSource(&config.Config{}).Name() != "oidc-federation" {
		t.Fatal("Name should be 'oidc-federation'")
	}
}

func TestOIDCFederationNoTokenIsNoCredential(t *testing.T) {
	clearOIDCEnv(t)
	src := NewOIDCFederationSource(&config.Config{AuthBase: "https://auth.api.zentoris.com"})
	if _, err := src.Token(context.Background()); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential with no CI OIDC token available", err)
	}
}

func TestOIDCFederationExplicitTokenReachesStub(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("ZENTORIS_OIDC_TOKEN", "ci-jwt")
	src := NewOIDCFederationSource(&config.Config{AuthBase: "https://auth.api.zentoris.com"})
	_, err := src.Token(context.Background())
	// The JWT is acquired, but the exchange is not built yet - a clear, non-ErrNoCredential error.
	if err == nil || errors.Is(err, ErrNoCredential) || !strings.Contains(err.Error(), "not built yet") {
		t.Fatalf("err = %v, want the 'exchange endpoint not built yet' error", err)
	}
}

func TestOIDCFederationTokenFromFile(t *testing.T) {
	clearOIDCEnv(t)
	path := filepath.Join(t.TempDir(), "jwt")
	if err := os.WriteFile(path, []byte("  file-jwt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZENTORIS_OIDC_TOKEN_FILE", path)
	src := NewOIDCFederationSource(&config.Config{AuthBase: "https://auth.api.zentoris.com"})
	if _, err := src.Token(context.Background()); err == nil || errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want the stub error (a token was read from the file)", err)
	}
}
