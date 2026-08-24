package auth

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

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

func TestPersistLoginStoresLabelsAndActivates(t *testing.T) {
	keyring.MockInit()
	withTempDir(t)

	enc := base64.RawURLEncoding.EncodeToString
	jwt := enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(`{"email":"me@zentoris.test"}`)) + ".sig"
	if err := persistLogin(&config.Config{Profile: "work"},
		&Credentials{AccessToken: jwt, RefreshToken: "rt", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	got, _ := NewStore().Load("work")
	if got == nil || got.AccessToken != jwt {
		t.Fatalf("stored creds = %+v, want the access token persisted", got)
	}
	if got.Subject != "me@zentoris.test" {
		t.Fatalf("subject = %q, want it parsed from the JWT at login", got.Subject)
	}
	if ActiveProfile() != "work" {
		t.Fatalf("active = %q, want work (a fresh login activates its profile)", ActiveProfile())
	}
}
