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
// parameters. Stored in DB for reference — NOT sent via SMS because
// mygov:// protocol can't be clicked in SMS on most phones.
func BuildDeeplink(clientID, nonce, state, redirectURI string) string {
        params := url.Values{}
        params.Set("client_id", clientID)
        params.Set("nonce", nonce)
        params.Set("state", state)
        params.Set("redirect_uri", redirectURI)
        encoded := params.Encode()
        // Replace _ with %5F — SMS gateways using GSM 7-bit strip underscores
        encoded = strings.ReplaceAll(encoded, "_", "%5F")
        return "mygov://consent?" + encoded
}

// BuildWebURL constructs a clickable HTTPS URL that the netlify web app
// will use to redirect the user to the mygov:// deeplink. This URL is
// sent via SMS because HTTPS links are clickable on all phones.
//
// The netlify app reads the query parameters and constructs the
// mygov:// deeplink from them, then triggers the MyGov app.
func BuildWebURL(webBaseURL, clientID, nonce, state, redirectURI string) string {
        params := url.Values{}
        params.Set("client_id", clientID)
        params.Set("nonce", nonce)
        params.Set("state", state)
        params.Set("redirect_uri", redirectURI)
        return webBaseURL + "?" + params.Encode()
}
