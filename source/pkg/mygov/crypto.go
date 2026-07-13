package mygov

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
)

// GenerateNonce generates a cryptographically secure random nonce.
func GenerateNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateState generates a cryptographically secure random state parameter.
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// BuildDeeplink constructs the mygov:// consent deeplink with all required
// parameters. Uses url.Values for proper URL encoding — prevents spaces
// and special characters from breaking the URL.
func BuildDeeplink(clientID, nonce, state, redirectURI string) string {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("nonce", nonce)
	params.Set("state", state)
	params.Set("redirect_uri", redirectURI)
	return "mygov://consent?" + params.Encode()
}
