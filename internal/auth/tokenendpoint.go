package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// The OP token endpoint serves several grants, and every one of them is the same exchange: POST a
// form, get back an access token and how long it lasts. Only the form differs, so that is the only
// thing a caller supplies - the address, the timeout, the error rendering, and how early a token is
// refreshed are decided once, here, rather than once per grant.
const (
	tokenEndpointTimeout = 15 * time.Second
	// defaultTokenTTL applies when the response omits expires_in (it is optional in RFC 6749).
	defaultTokenTTL = 5 * time.Minute
	// tokenRefreshMargin re-mints a little before expiry, so a token cannot lapse in flight.
	tokenRefreshMargin = 30 * time.Second
)

// postTokenEndpoint runs one grant against {AuthBase}/tenants/{opTenant}/oauth2/token and returns
// the issued token together with the moment a caller should stop reusing it (expiry less a safety
// margin, so callers need not re-derive it).
func postTokenEndpoint(ctx context.Context, cfg *config.Config, form url.Values) (token string, refreshAt time.Time, err error) {
	endpoint := fmt.Sprintf("%s/tenants/%s/oauth2/token",
		strings.TrimRight(cfg.AuthBase, "/"), opTenant)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := cfg.HTTPClient(tokenEndpointTimeout).Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, tokenEndpointError(resp.Status, data)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", time.Time{}, fmt.Errorf("decode token response: %w", err)
	}
	if body.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("token endpoint returned no access_token")
	}
	ttl := time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = defaultTokenTTL
	}
	return body.AccessToken, time.Now().Add(ttl - tokenRefreshMargin), nil
}

// tokenEndpointErr is a failure the token endpoint REPORTED (as opposed to a transport failure),
// carrying the RFC 6749 error code so a caller can add context to a code that needs it without
// re-parsing the body. The rendered message is what the user sees.
type tokenEndpointErr struct {
	code string
	msg  string
}

func (e *tokenEndpointErr) Error() string { return e.msg }

// tokenEndpointError renders an OP token-endpoint failure body (an RFC 6749 OAuth error or an
// RFC 9457 problem+json) into a readable message. Error bodies carry no token, so this is safe.
func tokenEndpointError(status string, body []byte) error {
	var p struct {
		Error     string `json:"error"`
		ErrorDesc string `json:"error_description"`
		Title     string `json:"title"`
		Detail    string `json:"detail"`
	}
	_ = json.Unmarshal(body, &p)
	var msg string
	switch {
	case p.ErrorDesc != "":
		msg = fmt.Sprintf("token endpoint %s: %s (%s)", status, p.ErrorDesc, p.Error)
	case p.Detail != "":
		msg = fmt.Sprintf("token endpoint %s: %s", status, p.Detail)
	case p.Error != "":
		msg = fmt.Sprintf("token endpoint %s: %s", status, p.Error)
	default:
		msg = fmt.Sprintf("token endpoint returned %s", status)
	}
	return &tokenEndpointErr{code: p.Error, msg: msg}
}
