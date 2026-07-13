package mygov

import (
        "crypto/rand"
        "encoding/base64"
        "fmt"
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

// BuildDeeplink constructs the mygov:// consent deeplink.
func BuildDeeplink(clientID, nonce, state, redirectURI string) string {
        return fmt.Sprintf("mygov://consent?client_id=%s&nonce=%s&state=%s&redirect_uri=%s",
                clientID, nonce, state, redirectURI)
}
