package auth

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

func TestLoginHint(t *testing.T) {
	cases := []struct {
		name            string
		profile, active string
		fromFlag, want  bool
	}{
		{"bare login into the active profile", "default", "default", false, false},
		{"env or active (not a flag)", "work", "default", false, false},
		{"--profile override that is not active", "work", "default", true, true},
		{"--profile that is already active", "work", "work", true, false},
	}
	for _, tc := range cases {
		got := loginHint(tc.profile, tc.fromFlag, tc.active)
		if (got != "") != tc.want {
			t.Errorf("%s: loginHint = %q, want a hint = %v", tc.name, got, tc.want)
		}
		if tc.want && !strings.Contains(got, tc.profile) {
			t.Errorf("%s: hint %q should name the profile", tc.name, got)
		}
	}
}

func TestLoginSourceName(t *testing.T) {
	if NewLoginSource(&config.Config{}).Name() != "login" {
		t.Fatal("LoginSource.Name should be 'login'")
	}
}

func TestClientCredentialsName(t *testing.T) {
	if NewClientCredentialsSource(&config.Config{}).Name() != "client-credentials" {
		t.Fatal("ClientCredentialsSource.Name should be 'client-credentials'")
	}
}

func TestExchangeCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("grant_type") != "authorization_code" || r.FormValue("code") != "the-code" ||
			r.FormValue("client_id") != "cli" || r.FormValue("code_verifier") != "verif" {
			t.Errorf("unexpected exchange form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
	}))
	defer srv.Close()

	creds, err := exchangeCode(context.Background(), &config.Config{}, srv.URL, "the-code", "verif", "http://127.0.0.1:9/cb")
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "at" || creds.RefreshToken != "rt" || creds.Expiry.IsZero() {
		t.Fatalf("creds = %+v, want at/rt with a populated expiry", creds)
	}
}

func TestDecodeTokenResponse(t *testing.T) {
	if _, err := decodeTokenResponse([]byte(`not json`)); err == nil {
		t.Error("want an error for a non-JSON body")
	}
	if _, err := decodeTokenResponse([]byte(`{"refresh_token":"rt"}`)); err == nil {
		t.Error("want an error when access_token is missing")
	}
	creds, err := decodeTokenResponse([]byte(`{"access_token":"at","expires_in":60}`))
	if err != nil || creds.AccessToken != "at" || creds.Expiry.IsZero() {
		t.Fatalf("got (%+v, %v), want at with a populated expiry", creds, err)
	}
}

func TestPersistLoginPassive(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	enc := base64.RawURLEncoding.EncodeToString
	jwt := enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(`{"email":"me@zentoris.test"}`)) + ".sig"
	cfg := &config.Config{Profile: "work"}
	if err := persistLogin(cfg, false, &Credentials{AccessToken: jwt, RefreshToken: "rt", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	// Login targets the explicitly-pinned profile and labels it with the account identity...
	got, _ := NewStore().Load("work")
	if got == nil || got.AccessToken != jwt {
		t.Fatalf("stored creds = %+v, want the token persisted under the target profile", got)
	}
	if got.Subject != "me@zentoris.test" {
		t.Fatalf("subject = %q, want it parsed from the JWT at login", got.Subject)
	}
	// ...but passive login must NOT change the active profile.
	if mustActive(t) != "default" {
		t.Fatalf("passive login must not activate; active = %q", mustActive(t))
	}
}

func TestPersistLoginActivate(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	cfg := &config.Config{Profile: "work"}
	if err := persistLogin(cfg, true, &Credentials{AccessToken: "at", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if mustActive(t) != "work" {
		t.Fatalf("--activate should switch the active profile to work; got %q", mustActive(t))
	}
}

func TestPersistLoginTargetsDefaultWhenUnpinned(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	// Nothing pinned (cfg.Profile empty) -> the login target normalizes to "default".
	if err := persistLogin(&config.Config{}, false, &Credentials{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	if b := NewStore().Backend("default"); b != "keychain" {
		t.Fatalf(`bare login should store under "default"; backend = %q`, b)
	}
	if mustActive(t) != "default" {
		t.Fatalf("bare passive login must not activate; active = %q", mustActive(t))
	}
}
