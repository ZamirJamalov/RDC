package mygov

import (
        "crypto/rand"
        "encoding/base64"
        "fmt"
        "net/url"
        "strings"
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
// parameters. Uses url.Values for proper URL encoding.
//
// IMPORTANT: SMS gateways using GSM 7-bit encoding strip underscores (_).
// We replace _ with %5F (URL-encoded underscore) so it survives SMS
// transmission. The MyGov app's URL parser will decode %5F back to _.
func BuildDeeplink(clientID, nonce, state, redirectURI string) string {
        params := url.Values{}
        params.Set("client_id", clientID)
        params.Set("nonce", nonce)
        params.Set("state", state)
        params.Set("redirect_uri", redirectURI)
        encoded := params.Encode()
        // Replace underscores with %5F to survive GSM 7-bit SMS encoding
        encoded = strings.ReplaceAll(encoded, "_", "%5F")
        return "mygov://consent?" + encoded
}
