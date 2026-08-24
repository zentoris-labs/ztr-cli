package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zentoris-labs/ztr-cli/internal/auth"
	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// tokenResolver builds a resolver that yields the given raw token (empty = no credential).
func tokenResolver(token string) *auth.Resolver {
	return auth.NewResolver(auth.NewRawTokenSource(&config.Config{Token: token}))
}

func TestDoSuccessSendsHeadersAndBody(t *testing.T) {
	var gotAuth, gotAccept, gotUA, gotCT, gotIfMatch, gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		gotCT = r.Header.Get("Content-Type")
		gotIfMatch = r.Header.Get("If-Match")
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"r1"}`)
	}))
	defer srv.Close()

	cfg := &config.Config{APIBase: srv.URL}
	c := New(cfg, tokenResolver("fake-token"), "test-9")

	var out struct {
		ID string `json:"id"`
	}
	if err := c.Do(context.Background(), http.MethodPatch, "/services/svc_1", map[string]string{"k": "v"}, "etag-1", &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "r1" {
		t.Fatalf("decoded id = %q, want r1", out.ID)
	}
	checks := map[string]struct{ got, want string }{
		"Authorization": {gotAuth, "Bearer fake-token"},
		"Accept":        {gotAccept, "application/json"},
		"User-Agent":    {gotUA, "zentoris/test-9"},
		"Content-Type":  {gotCT, "application/json"},
		"If-Match":      {gotIfMatch, "etag-1"},
		"method":        {gotMethod, http.MethodPatch},
		"path":          {gotPath, "/services/svc_1"},
		"body":          {strings.TrimSpace(gotBody), `{"k":"v"}`},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", name, c.got, c.want)
		}
	}
}

func TestDoOmitsIfMatchAndContentTypeWhenEmpty(t *testing.T) {
	var hadCT, hadIfMatch bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadCT = r.Header["Content-Type"]
		_, hadIfMatch = r.Header["If-Match"]
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := New(&config.Config{APIBase: srv.URL}, tokenResolver("fake-token"), "test")
	if err := c.Do(context.Background(), http.MethodGet, "/services", nil, "", nil); err != nil {
		t.Fatal(err)
	}
	if hadCT {
		t.Error("Content-Type must not be set for a body-less request")
	}
	if hadIfMatch {
		t.Error("If-Match must not be set when the ifMatch argument is empty")
	}
}

func TestDoTrimsTrailingSlashOnBase(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := New(&config.Config{APIBase: srv.URL + "/"}, tokenResolver("fake-token"), "test")
	if err := c.Do(context.Background(), http.MethodGet, "/x", nil, "", nil); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/x" {
		t.Fatalf("path = %q, want /x (no doubled slash)", gotPath)
	}
}

func TestDoRendersProblemJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `{"type":"about:blank#conflict","title":"Conflict","detail":"etag mismatch"}`)
	}))
	defer srv.Close()

	c := New(&config.Config{APIBase: srv.URL}, tokenResolver("fake-token"), "test")
	err := c.Do(context.Background(), http.MethodGet, "/x", nil, "", nil)
	if err == nil {
		t.Fatal("expected an error for a 409")
	}
	for _, want := range []string{"409", "Conflict", "etag mismatch", "about:blank#conflict"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestDoRendersPlainError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "upstream boom")
	}))
	defer srv.Close()

	c := New(&config.Config{APIBase: srv.URL}, tokenResolver("fake-token"), "test")
	err := c.Do(context.Background(), http.MethodGet, "/x", nil, "", nil)
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "upstream boom") {
		t.Fatalf("err = %v, want it to include the status and raw body", err)
	}
}

func TestDoPropagatesResolverError(t *testing.T) {
	// No server should be needed: resolution fails before any request is made.
	c := New(&config.Config{APIBase: "http://127.0.0.1:0"}, tokenResolver(""), "test")
	if err := c.Do(context.Background(), http.MethodGet, "/x", nil, "", nil); err == nil {
		t.Fatal("expected an error when no credential resolves")
	}
}

func TestParseProblem(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"title+detail+type", `{"type":"t","title":"T","detail":"D"}`, []string{"400", "T", "D", "t"}},
		{"title only", `{"title":"Just a title"}`, []string{"400", "Just a title"}},
		{"non-problem body", `not json at all`, []string{"400", "not json at all"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parseProblem(400, []byte(tc.body))
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%q missing %q", err.Error(), want)
				}
			}
		})
	}
}
