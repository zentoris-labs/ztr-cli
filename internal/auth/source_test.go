package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

type fakeSource struct {
	name string
	tok  string
	err  error
}

func (f fakeSource) Name() string { return f.name }
func (f fakeSource) Token(context.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.tok, nil
}

func TestResolverFirstAvailableWins(t *testing.T) {
	r := NewResolver(
		fakeSource{name: "a", tok: "tok-a"},
		fakeSource{name: "b", tok: "tok-b"},
	)
	tok, src, err := r.Token(context.Background())
	if err != nil || tok != "tok-a" || src != "a" {
		t.Fatalf("got (%q, %q, %v), want (tok-a, a, nil)", tok, src, err)
	}
}

func TestResolverSkipsNoCredential(t *testing.T) {
	r := NewResolver(
		fakeSource{name: "a", err: ErrNoCredential},
		fakeSource{name: "b", tok: "tok-b"},
	)
	tok, src, err := r.Token(context.Background())
	if err != nil || tok != "tok-b" || src != "b" {
		t.Fatalf("got (%q, %q, %v), want (tok-b, b, nil)", tok, src, err)
	}
}

func TestResolverHardErrorStops(t *testing.T) {
	// A non-ErrNoCredential error must halt the chain (not silently fall through to a later source).
	r := NewResolver(
		fakeSource{name: "a", err: errors.New("boom")},
		fakeSource{name: "b", tok: "tok-b"},
	)
	_, src, err := r.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "a: boom") {
		t.Fatalf("err = %v, want it to mention 'a: boom'", err)
	}
	if src != "a" {
		t.Fatalf("src = %q, want a", src)
	}
}

func TestResolverAllExhausted(t *testing.T) {
	r := NewResolver(
		fakeSource{name: "a", err: ErrNoCredential},
		fakeSource{name: "b", err: ErrNoCredential},
	)
	_, _, err := r.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "auth login") {
		t.Fatalf("err = %v, want the 'run zentoris auth login' guidance", err)
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("err = %v, want it to list the tried sources", err)
	}
}

func TestDefaultChainPrefersRawToken(t *testing.T) {
	// With a raw token set, the token source wins first - proving the chain order and that no
	// later source (login/store, client-creds, oidc) is consulted.
	tok, src, err := DefaultChain(&config.Config{Token: "fake-token"}).Token(context.Background())
	if err != nil || tok != "fake-token" || src != "token" {
		t.Fatalf("got (%q, %q, %v), want (fake-token, token, nil)", tok, src, err)
	}
}
