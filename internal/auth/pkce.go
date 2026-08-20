package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// randomURLSafe returns nBytes of CSPRNG entropy as an unpadded base64url string.
func randomURLSafe(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newPKCE returns a PKCE code_verifier and its S256 code_challenge (RFC 7636).
func newPKCE() (verifier, challenge string, err error) {
	verifier, err = randomURLSafe(32) // ~43 chars, within the 43-128 range
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}
