package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

func TestClientCredentialsNoCredsIsNoCredential(t *testing.T) {
	src := NewClientCredentialsSource(&config.Config{})
	if _, err := src.Token(context.Background()); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential without a client id/secret", err)
	}
}

func TestClientCredentialsMintsAndCaches(t *testing.T) {
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/tenants/main/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = r.ParseForm()
		if r.FormValue("grant_type") != "client_credentials" ||
			r.FormValue("client_id") != "cid" ||
			r.FormValue("client_secret") != "test-secret-not-real" {
			t.Errorf("unexpected token form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"cc-token","expires_in":3600}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := NewClientCredentialsSource(&config.Config{
		AuthBase: srv.URL, ClientID: "cid", ClientSecret: "test-secret-not-real",
	})
	tok, err := src.Token(context.Background())
	if err != nil || tok != "cc-token" {
		t.Fatalf("got (%q, %v), want (cc-token, nil)", tok, err)
	}
	// A second call within the token's lifetime is served from the in-memory cache.
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("token endpoint called %d times, want 1 (cached)", n)
	}
}

func TestClientCredentialsErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"invalid_client","error_description":"bad secret"}`)
	}))
	defer srv.Close()

	src := NewClientCredentialsSource(&config.Config{AuthBase: srv.URL, ClientID: "cid", ClientSecret: "nope"})
	_, err := src.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "bad secret") {
		t.Fatalf("err = %v, want it to render the error_description", err)
	}
}
