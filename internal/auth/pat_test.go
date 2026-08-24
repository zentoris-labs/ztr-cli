package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

func TestRawTokenSource(t *testing.T) {
	src := NewRawTokenSource(&config.Config{Token: "  fake-token  "})
	if src.Name() != "token" {
		t.Fatalf("name = %q, want token", src.Name())
	}
	tok, err := src.Token(context.Background())
	if err != nil || tok != "fake-token" {
		t.Fatalf("got (%q, %v), want (fake-token, nil) - surrounding whitespace trimmed", tok, err)
	}
}

func TestRawTokenSourceEmpty(t *testing.T) {
	src := NewRawTokenSource(&config.Config{Token: "   "})
	if _, err := src.Token(context.Background()); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential for a blank token", err)
	}
}
