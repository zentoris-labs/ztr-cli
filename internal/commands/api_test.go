package commands

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/zentoris-labs/ztr-cli/internal/api"
	"github.com/zentoris-labs/ztr-cli/internal/auth"
	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// apiDeps wires a deps whose API client points at a fake server and whose credential is a static
// token (so the resolver never touches the keychain or network). This is the injection seam that
// lets the command tests drive the real request/response path offline.
func apiDeps(t *testing.T, h http.HandlerFunc) (*deps, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := &config.Config{APIBase: srv.URL, Token: "fake-token", Account: "default"}
	resolver := auth.DefaultChain(cfg)
	return &deps{cfg: cfg, resolver: resolver, api: api.New(cfg, resolver, "test")}, srv
}

func run1(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestServiceListSuccess(t *testing.T) {
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/services" {
			t.Errorf("got %s %s, want GET /services", r.Method, r.URL.Path)
		}
		io.WriteString(w, `[{"id":"svc_1"}]`)
	})
	out, err := run1(t, newServiceListCmd(d))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "svc_1") {
		t.Fatalf("output %q, want the service id rendered", out)
	}
}

func TestServiceGetSuccess(t *testing.T) {
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/services/svc_9" {
			t.Errorf("path = %q, want /services/svc_9", r.URL.Path)
		}
		io.WriteString(w, `{"id":"svc_9","name":"api"}`)
	})
	out, err := run1(t, newServiceGetCmd(d), "svc_9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "svc_9") || !strings.Contains(out, "api") {
		t.Fatalf("output %q", out)
	}
}

func TestServiceUpdateSendsPatch(t *testing.T) {
	var gotBody, gotIfMatch, gotMethod string
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotIfMatch = r.Method, r.Header.Get("If-Match")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{}`)
	})
	out, err := run1(t, newServiceUpdateCmd(d), "svc_1", "--set", "A=B", "--if-match", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotIfMatch != "v1" {
		t.Errorf("If-Match = %q, want v1 (forwarded from --if-match)", gotIfMatch)
	}
	if !strings.Contains(gotBody, `"variables"`) || !strings.Contains(gotBody, `"A":"B"`) {
		t.Errorf("request body %q, want the variables patch", gotBody)
	}
	if !strings.Contains(out, "Updated 1 variable(s) on service svc_1") {
		t.Fatalf("output %q", out)
	}
}

func TestReleaseCreateSuccess(t *testing.T) {
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/services/svc_1/releases" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"id":"rel_1"}`)
	})
	out, err := run1(t, newReleaseCreateCmd(d), "--service", "svc_1", "--commit", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rel_1") {
		t.Fatalf("output %q, want the created release id", out)
	}
}

func TestReleaseListSuccess(t *testing.T) {
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/services/svc_1/releases" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, `[{"id":"rel_1"}]`)
	})
	out, err := run1(t, newReleaseListCmd(d), "--service", "svc_1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rel_1") {
		t.Fatalf("output %q", out)
	}
}

func TestAuthPrintAccessToken(t *testing.T) {
	d, _ := apiDeps(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, `{}`) })
	out, err := run1(t, newAuthPrintTokenCmd(d))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "fake-token" {
		t.Fatalf("output %q, want the resolved bearer token", out)
	}
}

func TestVersionCmd(t *testing.T) {
	out, err := run1(t, newVersionCmd())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "zentoris ") {
		t.Fatalf("output %q, want a version line", out)
	}
}
