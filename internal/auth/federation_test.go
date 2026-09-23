package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// testTrustID stands in for a federated-trust id: an identifier, not a credential.
const testTrustID = "trust-not-real"

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

// exchangeServer stands in for the Zentoris token endpoint, asserting the RFC 8693 form and
// recording how many exchanges happened. It returns the subject_token it was given, so a test
// can tell which provider's JWT reached the exchange.
func exchangeServer(t *testing.T, calls *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/tenants/main/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		_ = r.ParseForm()
		if g := r.FormValue("grant_type"); g != tokenExchangeGrant {
			t.Errorf("grant_type = %q, want %q", g, tokenExchangeGrant)
		}
		if t2 := r.FormValue("subject_token_type"); t2 != jwtTokenType {
			t.Errorf("subject_token_type = %q, want %q", t2, jwtTokenType)
		}
		if t2 := r.FormValue("requested_token_type"); t2 != accessTokenTokenType {
			t.Errorf("requested_token_type = %q, want %q", t2, accessTokenTokenType)
		}
		// The trust to assume is named in the request; without it the real endpoint rejects with
		// invalid_request before it verifies anything.
		if got := r.FormValue("trust_id"); got != testTrustID {
			t.Errorf("trust_id = %q, want %q", got, testTrustID)
		}
		// The exchange grant is anonymous - a client_id here would be a bug, not a nicety.
		if got := r.FormValue("client_id"); got != "" {
			t.Errorf("client_id = %q, want it absent (the exchange grant is anonymous)", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"exchanged-for-%s","expires_in":3600}`, r.FormValue("subject_token"))
	})
	return httptest.NewServer(mux)
}

func TestOIDCFederationExchangesExplicitTokenAndCaches(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("ZENTORIS_OIDC_TOKEN", "ci-jwt")
	var calls int32
	srv := exchangeServer(t, &calls)
	defer srv.Close()

	src := NewOIDCFederationSource(&config.Config{AuthBase: srv.URL, TrustID: testTrustID})
	tok, err := src.Token(context.Background())
	if err != nil || tok != "exchanged-for-ci-jwt" {
		t.Fatalf("got (%q, %v), want (exchanged-for-ci-jwt, nil)", tok, err)
	}
	// A second call within the token's lifetime is served from the in-memory cache.
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("token endpoint called %d times, want 1 (cached)", n)
	}
}

func TestOIDCFederationTokenFromFile(t *testing.T) {
	clearOIDCEnv(t)
	path := filepath.Join(t.TempDir(), "jwt")
	if err := os.WriteFile(path, []byte("  file-jwt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZENTORIS_OIDC_TOKEN_FILE", path)
	var calls int32
	srv := exchangeServer(t, &calls)
	defer srv.Close()

	src := NewOIDCFederationSource(&config.Config{AuthBase: srv.URL, TrustID: testTrustID})
	tok, err := src.Token(context.Background())
	if err != nil || tok != "exchanged-for-file-jwt" {
		t.Fatalf("got (%q, %v), want the trimmed file JWT to be exchanged", tok, err)
	}
}

func TestOIDCFederationExchangesGitHubActionsToken(t *testing.T) {
	clearOIDCEnv(t)
	var calls int32
	exchange := exchangeServer(t, &calls)
	defer exchange.Close()

	// Stand in for the Actions token service: it must be called with the request token and an
	// audience naming the Zentoris auth base.
	actions := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fake-request-token" {
			t.Errorf("Authorization = %q, want the request token", got)
		}
		if got := r.URL.Query().Get("audience"); got != exchange.URL {
			t.Errorf("audience = %q, want %q", got, exchange.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"value":"gha-jwt"}`)
	}))
	defer actions.Close()

	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", actions.URL)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "fake-request-token")

	src := NewOIDCFederationSource(&config.Config{AuthBase: exchange.URL, TrustID: testTrustID})
	tok, err := src.Token(context.Background())
	if err != nil || tok != "exchanged-for-gha-jwt" {
		t.Fatalf("got (%q, %v), want (exchanged-for-gha-jwt, nil)", tok, err)
	}
}

func TestOIDCFederationExchangeErrorNamesTheProvider(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("ZENTORIS_OIDC_TOKEN", "ci-jwt")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":"invalid_grant","error_description":"no trust policy matches these claims"}`)
	}))
	defer srv.Close()

	src := NewOIDCFederationSource(&config.Config{AuthBase: srv.URL, TrustID: testTrustID})
	_, err := src.Token(context.Background())
	// A rejected exchange is a hard failure, not ErrNoCredential: the chain must not silently
	// fall through and report "not logged in" when CI did present an identity.
	if err == nil || errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want a hard exchange error", err)
	}
	if !strings.Contains(err.Error(), "explicit") || !strings.Contains(err.Error(), "no trust policy matches these claims") {
		t.Fatalf("err = %v, want it to name the provider and render the error_description", err)
	}
}

func TestOIDCFederationExchangeWithoutAccessToken(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("ZENTORIS_OIDC_TOKEN", "ci-jwt")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"expires_in":3600}`)
	}))
	defer srv.Close()

	src := NewOIDCFederationSource(&config.Config{AuthBase: srv.URL, TrustID: testTrustID})
	if _, err := src.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "no access_token") {
		t.Fatalf("err = %v, want a complaint about the missing access_token", err)
	}
}

func TestOIDCFederationWithoutTrustIDIsAMisconfiguration(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("ZENTORIS_OIDC_TOKEN", "ci-jwt")
	var calls int32
	srv := exchangeServer(t, &calls)
	defer srv.Close()

	// A CI identity is present but no trust is named: fail with a message that says what to set,
	// rather than falling through to "no credential found" or letting the server answer with its
	// uniform reject. Nothing is sent to the token endpoint.
	src := NewOIDCFederationSource(&config.Config{AuthBase: srv.URL})
	_, err := src.Token(context.Background())
	if err == nil || errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want a hard misconfiguration error", err)
	}
	if !strings.Contains(err.Error(), "ZENTORIS_TRUST_ID") {
		t.Fatalf("err = %v, want it to name ZENTORIS_TRUST_ID", err)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("token endpoint called %d times, want 0", n)
	}
}
