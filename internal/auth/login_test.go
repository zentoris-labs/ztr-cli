package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

func TestLoginSourceSilentlyRefreshes(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	var refreshCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/tenants/main/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"authorization_endpoint":%q,"token_endpoint":%q}`, base+"/authorize", base+"/token")
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") != "rt-1" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		atomic.AddInt32(&refreshCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"at-new","refresh_token":"rt-2","expires_in":3600}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := &config.Config{AuthBase: srv.URL, Profile: "work"}
	store := NewStore()
	// An already-expired access token, but with a valid refresh token to renew from.
	if err := store.Save("work", &Credentials{AccessToken: "at-old", RefreshToken: "rt-1", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	src := NewLoginSource(cfg)
	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "at-new" {
		t.Fatalf("token = %q, want at-new (silently refreshed)", tok)
	}
	// The rotated credentials must be persisted so the next command reuses them.
	got, _ := store.Load("work")
	if got == nil || got.AccessToken != "at-new" || got.RefreshToken != "rt-2" {
		t.Fatalf("persisted creds = %+v, want at-new/rt-2", got)
	}

	// A second call now finds a valid token and must NOT refresh again.
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&refreshCalls); n != 1 {
		t.Fatalf("refresh called %d times, want exactly 1", n)
	}
}

func TestLoginSourceExpiredNoRefreshToken(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	cfg := &config.Config{Profile: "work"}
	if err := NewStore().Save("work", &Credentials{AccessToken: "at-old", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLoginSource(cfg).Token(context.Background()); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("expired token with no refresh token: err = %v, want ErrNoCredential", err)
	}
}

func TestNewPKCE(t *testing.T) {
	v, c, err := newPKCE()
	if err != nil {
		t.Fatal(err)
	}
	if v == "" || c == "" {
		t.Fatal("empty pkce output")
	}
	if v == c {
		t.Fatal("verifier equals challenge")
	}
	sum := sha256.Sum256([]byte(v))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); c != want {
		t.Fatalf("challenge is not S256(verifier): %q != %q", c, want)
	}
}

func TestAuthorizeURL(t *testing.T) {
	got := authorizeURL("https://login.test/web/auth/authorize", "http://127.0.0.1:5555/callback", "chal", "st")
	for _, want := range []string{
		"https://login.test/web/auth/authorize?",
		"response_type=code",
		"client_id=cli",
		"code_challenge=chal",
		"code_challenge_method=S256",
		"state=st",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("authorize url %q missing %q", got, want)
		}
	}
}

func TestWaitForCallbackSuccess(t *testing.T) {
	ln := mustListen(t)
	defer ln.Close()
	const state = "s-123"

	codeCh, errCh := make(chan string, 1), make(chan error, 1)
	go func() {
		code, err := waitForCallback(context.Background(), ln, state)
		if err != nil {
			errCh <- err
			return
		}
		codeCh <- code
	}()

	hitCallback(t, ln, "state="+state+"&code=the-code")

	select {
	case code := <-codeCh:
		if code != "the-code" {
			t.Fatalf("got code %q, want the-code", code)
		}
	case e := <-errCh:
		t.Fatalf("unexpected error: %v", e)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestWaitForCallbackStateMismatch(t *testing.T) {
	ln := mustListen(t)
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := waitForCallback(context.Background(), ln, "expected")
		errCh <- err
	}()

	hitCallback(t, ln, "state=wrong&code=x")

	select {
	case e := <-errCh:
		if e == nil {
			t.Fatal("expected an error on state mismatch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

// --- helpers ---

func mustListen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func hitCallback(t *testing.T, ln net.Listener, query string) {
	t.Helper()
	u := fmt.Sprintf("http://%s/callback?%s", ln.Addr().String(), query)
	var err error
	for i := 0; i < 50; i++ {
		var resp *http.Response
		if resp, err = http.Get(u); err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("callback GET never succeeded: %v", err)
}

// --- RFC 8628 device flow ---

func TestOAuthErrorCode(t *testing.T) {
	if got := oauthErrorCode([]byte(`{"error":"slow_down"}`)); got != "slow_down" {
		t.Fatalf("got %q, want slow_down", got)
	}
	if got := oauthErrorCode([]byte("not json")); got != "" {
		t.Fatalf("got %q, want empty for non-json", got)
	}
}

func TestRequestDeviceAuthorization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("client_id") != "cli" {
			t.Errorf("missing client_id, got form %v", r.Form)
		}
		// The CLI sends its hostname as an optional device_label so the account roster can name the
		// machine; the host running the test always has one, so it must be present and non-empty.
		if r.FormValue("device_label") == "" {
			t.Errorf("missing device_label, got form %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"device_code":"dc","user_code":"WDJB-MJHT",`+
			`"verification_uri":"https://acct.test/device",`+
			`"verification_uri_complete":"https://acct.test/device?user_code=WDJB-MJHT",`+
			`"expires_in":900,"interval":5}`)
	}))
	defer srv.Close()

	da, err := requestDeviceAuthorization(context.Background(), &config.Config{}, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if da.DeviceCode != "dc" || da.UserCode != "WDJB-MJHT" || da.Interval != 5 {
		t.Fatalf("bad device authorization: %+v", da)
	}
}

func TestRequestDeviceAuthorizationDefaultsInterval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"device_code":"dc","user_code":"UC","verification_uri":"https://x/device"}`)
	}))
	defer srv.Close()

	da, err := requestDeviceAuthorization(context.Background(), &config.Config{}, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if da.Interval != 5 {
		t.Fatalf("interval default: got %d, want 5", da.Interval)
	}
}

func TestRequestDeviceAuthorizationMissingFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"user_code":"UC","verification_uri":"https://x/device"}`) // no device_code
	}))
	defer srv.Close()

	if _, err := requestDeviceAuthorization(context.Background(), &config.Config{}, srv.URL); err == nil {
		t.Fatal("expected an error when device_code is missing")
	}
}

func TestPollDeviceTokenPendingThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("grant_type") != deviceCodeGrantType || r.FormValue("device_code") != "dev-code" {
			t.Errorf("unexpected poll form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"authorization_pending"}`)
			return
		}
		io.WriteString(w, `{"access_token":"at-123","refresh_token":"rt-123","expires_in":900}`)
	}))
	defer srv.Close()

	creds, err := pollDeviceToken(context.Background(), &config.Config{},
		srv.URL, "dev-code", 2*time.Millisecond, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "at-123" || creds.RefreshToken != "rt-123" {
		t.Fatalf("bad creds: %+v", creds)
	}
	if creds.Expiry.IsZero() {
		t.Fatal("expiry not populated from expires_in")
	}
	if atomic.LoadInt32(&calls) < 3 {
		t.Fatalf("expected the pending polls to precede success, saw %d calls", calls)
	}
}

func TestPollDeviceTokenSlowDownBacksOff(t *testing.T) {
	old := deviceSlowDownIncrement
	deviceSlowDownIncrement = 2 * time.Millisecond // keep the backoff test fast
	defer func() { deviceSlowDownIncrement = old }()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"slow_down"}`)
			return
		}
		io.WriteString(w, `{"access_token":"at-ok"}`)
	}))
	defer srv.Close()

	creds, err := pollDeviceToken(context.Background(), &config.Config{},
		srv.URL, "dev-code", 2*time.Millisecond, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "at-ok" {
		t.Fatalf("bad creds: %+v", creds)
	}
}

func TestPollDeviceTokenTerminalErrors(t *testing.T) {
	for _, tc := range []struct{ oauthErr, wantSubstr string }{
		{"access_denied", "denied"},
		{"expired_token", "expired"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q}`, tc.oauthErr)
		}))
		_, err := pollDeviceToken(context.Background(), &config.Config{},
			srv.URL, "dev-code", 2*time.Millisecond, time.Now().Add(2*time.Second))
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Fatalf("%s: got err %v, want it to mention %q", tc.oauthErr, err, tc.wantSubstr)
		}
	}
}

func TestPollDeviceTokenDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"authorization_pending"}`)
	}))
	defer srv.Close()

	// Deadline already in the past: the first interval wait elapses, the deadline check fires.
	_, err := pollDeviceToken(context.Background(), &config.Config{},
		srv.URL, "dev-code", 2*time.Millisecond, time.Now().Add(-time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("got err %v, want an expiry error", err)
	}
}

func TestPollDeviceTokenOnceUnstructuredError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "upstream boom") // no OAuth `error` field
	}))
	defer srv.Close()

	creds, code, err := pollDeviceTokenOnce(context.Background(), &config.Config{}, srv.URL, "dev-code")
	if creds != nil || code != "" {
		t.Fatalf("expected a hard error, got creds=%v code=%q", creds, code)
	}
	if err == nil {
		t.Fatal("expected an error for an unstructured 500")
	}
}
