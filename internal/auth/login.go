package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// The CLI is Zentoris's own first-party tooling, so its OAuth client id, login scope, and tenant
// are FIXED, not user-configurable (mirrors the console's fixed `console` client). The first-party
// client is provisioned with this well-known id; the minted session is platform-audienced and
// full-scope regardless of the requested scope, so `loginScope` is a formality the OP needs
// syntactically. Only the deployment (its --domain) varies per invocation, never the client or tenant.
const (
	clientID   = "cli"
	loginScope = "openid profile offline_access"
	opTenant   = "main" // fixed tenant segment in OP endpoint paths (/tenants/<opTenant>/...)
)

// LoginSource returns the access token cached by `zentoris auth login`. The interactive ceremony
// itself (RunInteractiveLogin) is a loopback-PKCE public-client sign-in against the Zentoris
// OP, which already supports RFC 8252 public clients - so this is CLI wiring, not backend work.
type LoginSource struct {
	cfg   *config.Config
	store *Store
}

// NewLoginSource builds the source over the per-user credential store.
func NewLoginSource(cfg *config.Config) *LoginSource {
	return &LoginSource{cfg: cfg, store: NewStore()}
}

func (s *LoginSource) Name() string { return "login" }

func (s *LoginSource) Token(ctx context.Context) (string, error) {
	creds, err := s.store.Load(s.cfg.Profile)
	if err != nil || creds == nil || creds.AccessToken == "" {
		return "", ErrNoCredential
	}
	if !tokenNeedsRefresh(creds) {
		return creds.AccessToken, nil
	}
	// The access token has expired (or is within the refresh skew). Renew it silently with the
	// stored refresh token; only if that is missing or rejected do we fall back to a fresh login.
	if creds.RefreshToken == "" {
		return "", ErrNoCredential
	}

	var token string
	lockErr := withProfileLock(s.cfg.Profile, func() error {
		// Re-read inside the lock: a concurrent invocation may have refreshed while we waited, in
		// which case we reuse its result instead of refreshing (and rotating) a second time.
		if cur, err := s.store.Load(s.cfg.Profile); err == nil && cur != nil && !tokenNeedsRefresh(cur) {
			token = cur.AccessToken
			return nil
		}
		refreshed, err := refreshTokens(ctx, s.cfg, creds.RefreshToken)
		if err != nil {
			return err
		}
		// Keep the identity label if the renewed token does not carry a readable one.
		if refreshed.Subject = subjectFromToken(refreshed.AccessToken); refreshed.Subject == "" {
			refreshed.Subject = creds.Subject
		}
		if err := s.store.Save(s.cfg.Profile, refreshed); err != nil {
			return err
		}
		token = refreshed.AccessToken
		return nil
	})
	if lockErr != nil {
		// A rejected refresh token (expired/revoked), a lock timeout, or a transient token-endpoint
		// error all land here: yield no credential so the resolver falls through and, if nothing
		// else authenticates, the user is told to sign in again.
		return "", ErrNoCredential
	}
	return token, nil
}

// tokenRefreshSkew renews a login shortly before its access token actually expires, so a token is
// never handed to the API with only seconds of life left.
const tokenRefreshSkew = 60 * time.Second

// tokenNeedsRefresh reports whether the stored access token has expired or is within the refresh
// skew. A zero expiry (unknown lifetime) is treated as still valid - the CLI cannot know better.
func tokenNeedsRefresh(c *Credentials) bool {
	return !c.Expiry.IsZero() && time.Now().After(c.Expiry.Add(-tokenRefreshSkew))
}

const loginCallbackTimeout = 3 * time.Minute

// RunInteractiveLogin performs a browser loopback-PKCE sign-in against the Zentoris OP and
// caches the resulting tokens for the active profile. The OP accepts RFC 8252 public clients
// (PKCE S256, loopback-any-port), so this needs no client secret and no backend change - it
// does require the fixed first-party `cli` client (see clientID) provisioned in the tenant.
func RunInteractiveLogin(ctx context.Context, cfg *config.Config) error {
	verifier, challenge, err := newPKCE()
	if err != nil {
		return err
	}
	state, err := randomURLSafe(24)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start local callback server: %w", err)
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://%s/callback", ln.Addr().String())

	endpoints, err := discover(ctx, cfg)
	if err != nil {
		return err
	}

	authURL := authorizeURL(endpoints.AuthorizationEndpoint, redirect, challenge, state)
	fmt.Fprintf(os.Stderr, "Opening your browser to sign in.\nIf it does not open, visit:\n\n  %s\n\n", authURL)
	_ = openBrowser(authURL)

	ctx, cancel := context.WithTimeout(ctx, loginCallbackTimeout)
	defer cancel()
	code, err := waitForCallback(ctx, ln, state)
	if err != nil {
		return err
	}

	creds, err := exchangeCode(ctx, cfg, endpoints.TokenEndpoint, code, verifier, redirect)
	if err != nil {
		return err
	}
	return persistLogin(cfg, creds)
}

// oidcConfig is the subset of the OIDC discovery document zentoris needs.
type oidcConfig struct {
	AuthorizationEndpoint       string `json:"authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}

// deviceCodeGrantType is the RFC 8628 device-authorization grant type used at the token endpoint.
const deviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// discover fetches the tenant's OIDC discovery document so login uses the endpoints the OP
// advertises rather than hardcoded paths - the standards-correct native-app approach. It also
// handles topologies where the browser authorization origin differs from the back-channel token
// origin (as it does locally: the login UI is a separate SPA host from the auth API).
func discover(ctx context.Context, cfg *config.Config) (*oidcConfig, error) {
	u := fmt.Sprintf("%s/tenants/%s/.well-known/openid-configuration",
		strings.TrimRight(cfg.AuthBase, "/"), opTenant)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := cfg.HTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery %s returned %s", u, resp.Status)
	}
	var oc oidcConfig
	if err := json.NewDecoder(resp.Body).Decode(&oc); err != nil {
		return nil, fmt.Errorf("decode discovery: %w", err)
	}
	if oc.AuthorizationEndpoint == "" || oc.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery missing authorization_endpoint or token_endpoint")
	}
	return &oc, nil
}

// authorizeURL appends the PKCE authorization-request params to the discovered
// authorization_endpoint.
func authorizeURL(endpoint, redirect, challenge, state string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirect},
		"scope":                 {loginScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	return endpoint + sep + q.Encode()
}

// waitForCallback serves the loopback redirect on ln until the OP redirects back with a code
// (validated against state), an error, ctx cancellation, or timeout.
func waitForCallback(ctx context.Context, ln net.Listener, state string) (string, error) {
	type result struct {
		code string
		err  error
	}
	resCh := make(chan result, 1)
	send := func(r result) {
		select {
		case resCh <- r:
		default: // a result is already captured; ignore extra requests (e.g. favicon)
		}
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("error") != "":
			writeBrowserPage(w, false, "Sign-in failed", "Something went wrong during authorization. You can close this tab and try again.")
			send(result{err: fmt.Errorf("authorization error: %s %s", q.Get("error"), q.Get("error_description"))})
		case q.Get("state") != state:
			writeBrowserPage(w, false, "Sign-in failed", "The response did not match this sign-in request. You can close this tab and try again.")
			send(result{err: fmt.Errorf("state mismatch on callback")})
		case q.Get("code") == "":
			writeBrowserPage(w, false, "Sign-in failed", "No authorization code was returned. You can close this tab and try again.")
			send(result{err: fmt.Errorf("no authorization code on callback")})
		default:
			writeBrowserPage(w, true, "You're signed in", "You can close this tab and return to your terminal.")
			send(result{code: q.Get("code")})
		}
	})}
	go func() { _ = srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		_ = srv.Close()
		return "", fmt.Errorf("waiting for browser sign-in: %w", ctx.Err())
	case res := <-resCh:
		_ = srv.Shutdown(context.Background())
		return res.code, res.err
	}
}

// writeBrowserPage renders the loopback callback result as a small, self-contained, theme-aware
// page (the tab the browser lands on after sign-in). It is served by the CLI's own 127.0.0.1
// listener, so everything is inlined - no external assets. heading/detail are CLI-controlled
// literals (never user input), so no escaping is needed.
func writeBrowserPage(w http.ResponseWriter, ok bool, heading, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Connection", "close")
	accent, icon := "#dc2626", `<path d="M18 6 6 18M6 6l12 12"/>`
	if ok {
		accent, icon = "#16a34a", `<path d="M20 6 9 17l-5-5"/>`
	}
	page := browserPage
	for old, new := range map[string]string{
		"{{ACCENT}}": accent, "{{ICON}}": icon, "{{HEADING}}": heading, "{{DETAIL}}": detail,
	} {
		page = strings.ReplaceAll(page, old, new)
	}
	_, _ = io.WriteString(w, page)
}

// browserPage is the loopback callback template: a centered card with a success/error badge and the
// zentoris wordmark, light/dark aware. Placeholders are filled by writeBrowserPage.
const browserPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Zentoris CLI</title>
<style>
  :root{--bg:#f6f7f9;--card:#fff;--text:#12151a;--muted:#667085;--border:#e6e8eb;--accent:{{ACCENT}}}
  @media (prefers-color-scheme:dark){
    :root{--bg:#0c0e11;--card:#15181d;--text:#e8eaed;--muted:#98a1ad;--border:#252a31}
  }
  *{box-sizing:border-box}
  html,body{height:100%}
  body{margin:0;background:var(--bg);color:var(--text);display:flex;align-items:center;justify-content:center;padding:24px;
       font:15px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
  .card{width:100%;max-width:420px;background:var(--card);border:1px solid var(--border);border-radius:16px;
        padding:40px 32px;text-align:center;box-shadow:0 10px 34px rgba(0,0,0,.07)}
  .badge{width:64px;height:64px;margin:0 auto 20px;border-radius:50%;display:flex;align-items:center;justify-content:center;
         background:color-mix(in srgb,var(--accent) 15%,transparent)}
  .badge svg{width:32px;height:32px;fill:none;stroke:var(--accent);stroke-width:2.5;stroke-linecap:round;stroke-linejoin:round}
  h1{margin:0 0 8px;font-size:20px;font-weight:650}
  p{margin:0;color:var(--muted)}
  .brand{margin-top:28px;font-size:12px;letter-spacing:.14em;text-transform:uppercase;color:var(--muted)}
  .brand b{color:var(--text);font-weight:600;letter-spacing:.06em}
</style>
</head>
<body>
  <main class="card">
    <div class="badge"><svg viewBox="0 0 24 24" aria-hidden="true">{{ICON}}</svg></div>
    <h1>{{HEADING}}</h1>
    <p>{{DETAIL}}</p>
    <div class="brand"><b>zentoris</b> &middot; CLI</div>
  </main>
</body>
</html>`

// exchangeCode swaps the authorization code for tokens at the discovered token endpoint.
func exchangeCode(ctx context.Context, cfg *config.Config, endpoint, code, verifier, redirect string) (*Credentials, error) {
	return postTokenForm(ctx, cfg, endpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	})
}

// refreshTokens renews an expired login by exchanging its stored refresh token for a fresh access
// token (RFC 6749 refresh_token grant), so the CLI renews silently instead of forcing a new
// browser sign-in. The OP may or may not rotate the refresh token; the old one is kept if none
// comes back, so the next renewal still has a token to present.
func refreshTokens(ctx context.Context, cfg *config.Config, refreshToken string) (*Credentials, error) {
	endpoints, err := discover(ctx, cfg)
	if err != nil {
		return nil, err
	}
	creds, err := postTokenForm(ctx, cfg, endpoints.TokenEndpoint, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"scope":         {loginScope},
	})
	if err != nil {
		return nil, err
	}
	if creds.RefreshToken == "" {
		creds.RefreshToken = refreshToken
	}
	return creds, nil
}

// postTokenForm POSTs a form-encoded grant to the OP token endpoint and decodes the successful
// response into Credentials, rendering an OAuth / problem+json error body otherwise.
func postTokenForm(ctx context.Context, cfg *config.Config, endpoint string, form url.Values) (*Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := cfg.HTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, tokenEndpointError(resp.Status, data)
	}
	return decodeTokenResponse(data)
}

// decodeTokenResponse parses a successful OAuth token-endpoint body into Credentials. Shared by
// the authorization_code exchange, the device-code poll, and the refresh-token renewal.
func decodeTokenResponse(data []byte) (*Credentials, error) {
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if body.AccessToken == "" {
		return nil, fmt.Errorf("token endpoint returned no access_token")
	}
	creds := &Credentials{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken}
	if body.ExpiresIn > 0 {
		creds.Expiry = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	return creds, nil
}

// deviceSlowDownIncrement is how much the poll interval grows on an RFC 8628 §3.5 slow_down
// (the spec mandates +5s). A package var only so tests can shrink it; production keeps 5s.
var deviceSlowDownIncrement = 5 * time.Second

// deviceAuthorization is the RFC 8628 §3.2 device-authorization response.
type deviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// RunDeviceLogin performs an RFC 8628 device-authorization sign-in: no local browser or loopback
// server, so it works over SSH / in containers / on headless boxes. The CLI prints a code + URL,
// the user approves in any browser (the account app's /device page), and the CLI polls the token
// endpoint until the approval lands. Mints the SAME platform session as the loopback path.
func RunDeviceLogin(ctx context.Context, cfg *config.Config) error {
	endpoints, err := discover(ctx, cfg)
	if err != nil {
		return err
	}
	if endpoints.DeviceAuthorizationEndpoint == "" {
		return fmt.Errorf("this tenant's OP does not advertise a device_authorization_endpoint")
	}

	da, err := requestDeviceAuthorization(ctx, cfg, endpoints.DeviceAuthorizationEndpoint)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nTo sign in, open:\n\n  %s\n\nand enter the code:\n\n  %s\n",
		da.VerificationURI, da.UserCode)
	if da.VerificationURIComplete != "" {
		// Also print the pre-filled link (code embedded) so copying it to a browser on THIS device needs
		// no manual entry. The plain URL + code above stay: RFC 8628 5.4 wants the user_code visible to
		// verify it matches the page (the complete link is a convenience, never a replacement).
		fmt.Fprintf(os.Stderr, "\nOr open this pre-filled link:\n\n  %s\n", da.VerificationURIComplete)
		// Best-effort: on a machine WITH a browser this opens the pre-filled page; headless boxes
		// (the reason to use this flow) just ignore the failure and rely on the printed URLs above.
		_ = openBrowser(da.VerificationURIComplete)
	}
	fmt.Fprintln(os.Stderr, "\nWaiting for approval...")

	interval := time.Duration(da.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(da.ExpiresIn) * time.Second)
	if da.ExpiresIn <= 0 {
		deadline = time.Now().Add(15 * time.Minute)
	}
	creds, err := pollDeviceToken(ctx, cfg, endpoints.TokenEndpoint, da.DeviceCode, interval, deadline)
	if err != nil {
		return err
	}
	return persistLogin(cfg, creds)
}

// persistLogin labels the credentials with the account identity (best-effort), stores them under
// the active profile, and records that profile as the active default (a fresh login switches to
// it). Shared by the loopback and device-code flows.
func persistLogin(cfg *config.Config, creds *Credentials) error {
	creds.Subject = subjectFromToken(creds.AccessToken)
	if err := NewStore().Save(cfg.Profile, creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	if err := RegisterLogin(cfg.Profile); err != nil {
		return fmt.Errorf("record profile: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Signed in. Credentials saved for profile %q.\n", cfg.Profile)
	return nil
}

// requestDeviceAuthorization starts the RFC 8628 flow at the device_authorization endpoint.
func requestDeviceAuthorization(ctx context.Context, cfg *config.Config, endpoint string) (*deviceAuthorization, error) {
	form := url.Values{
		"client_id": {clientID},
		"scope":     {loginScope},
	}
	// Optional machine label so the account session roster can name WHICH machine this CLI login is on
	// (revocation is then actionable across machines). Best-effort: omit it if the host has no name.
	if host, err := os.Hostname(); err == nil && host != "" {
		form.Set("device_label", host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := cfg.HTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("device authorization endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, tokenEndpointError(resp.Status, data)
	}
	var da deviceAuthorization
	if err := json.Unmarshal(data, &da); err != nil {
		return nil, fmt.Errorf("decode device authorization response: %w", err)
	}
	if da.DeviceCode == "" || da.UserCode == "" || da.VerificationURI == "" {
		return nil, fmt.Errorf("device authorization response missing device_code, user_code or verification_uri")
	}
	if da.Interval <= 0 {
		da.Interval = 5 // RFC 8628 §3.2 default when the server omits it
	}
	return &da, nil
}

// pollDeviceToken polls the token endpoint until the user approves (tokens), denies / expires
// (error), or the deadline passes. Honors the RFC 8628 §3.5 poll states: authorization_pending
// keeps waiting, slow_down widens the interval, access_denied / expired_token are terminal.
func pollDeviceToken(ctx context.Context, cfg *config.Config, endpoint, deviceCode string, interval time.Duration, deadline time.Time) (*Credentials, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("device authorization expired before it was approved")
		}

		// Poll first (the user may have approved already), then wait `interval` before retrying -
		// the backend's first poll never trips slow_down, and subsequent polls stay a full interval
		// apart so they don't either.
		creds, code, err := pollDeviceTokenOnce(ctx, cfg, endpoint, deviceCode)
		if err != nil {
			return nil, err
		}
		if creds != nil {
			return creds, nil
		}
		switch code {
		case "authorization_pending":
			// Not decided yet - keep polling on the same interval.
		case "slow_down":
			interval += deviceSlowDownIncrement
		case "access_denied":
			return nil, fmt.Errorf("the sign-in request was denied")
		case "expired_token":
			return nil, fmt.Errorf("the device code expired before it was approved")
		default:
			return nil, fmt.Errorf("device token poll failed: %s", code)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// pollDeviceTokenOnce does one device-code token request. It returns (creds, "", nil) on success,
// (nil, "<oauth error>", nil) for a structured OAuth error the caller dispatches on, or a non-nil
// error for a transport / unstructured failure.
func pollDeviceTokenOnce(ctx context.Context, cfg *config.Config, endpoint, deviceCode string) (*Credentials, string, error) {
	form := url.Values{
		"grant_type":  {deviceCodeGrantType},
		"client_id":   {clientID},
		"device_code": {deviceCode},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := cfg.HTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		creds, derr := decodeTokenResponse(data)
		return creds, "", derr
	}
	if code := oauthErrorCode(data); code != "" {
		return nil, code, nil
	}
	return nil, "", tokenEndpointError(resp.Status, data)
}

// oauthErrorCode extracts the RFC 6749 `error` code from a token-endpoint error body ("" if absent).
func oauthErrorCode(body []byte) string {
	var p struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &p)
	return p.Error
}
