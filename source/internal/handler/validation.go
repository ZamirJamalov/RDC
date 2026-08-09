package handler

import (
	"regexp"
	"strings"
)

// PR #149: Input validation regex patterns
var (
	// FIN kodu: 7 hərf/rəqəm (A-Z, 0-9)
	pinRegex = regexp.MustCompile(`^[A-Za-z0-9]{7}$`)
	// Seriya nömrəsi: 7-8 rəqəm
	serialNumberRegex = regexp.MustCompile(`^[0-9]{7,8}$`)
	// Telefon: +994 və 9 rəqəm (və ya sadəcə 9 rəqəm)
	phoneRegex = regexp.MustCompile(`^(\+994)?[0-9]{9}$`)
)

// isValidPIN checks if a FIN code is valid (7 alphanumeric characters).
func isValidPIN(pin string) bool {
	return pinRegex.MatchString(strings.TrimSpace(pin))
}

// isValidSerialNumber checks if a serial number is valid (7-8 digits).
func isValidSerialNumber(serial string) bool {
	return serialNumberRegex.MatchString(strings.TrimSpace(serial))
}

// isValidPhone checks if a phone number is valid (Azerbaijan format).
func isValidPhone(phone string) bool {
	// Remove spaces and dashes
	clean := strings.NewReplacer(" ", "", "-", "").Replace(phone)
	return phoneRegex.MatchString(clean)
}

// sanitizeError returns a generic error message for internal errors,
// while logging the real error. PR #149: don't expose internal details to client.
func sanitizeError(err error) string {
	// For known validation errors, return as-is
	// For internal errors, return generic message
	if err == nil {
		return ""
	}
	msg := err.Error()
	// If error contains sensitive info (SQL, connection, etc.), return generic
	if containsAny(msg, []string{"sql:", "database", "connection", "timeout", "panic", "runtime"}) {
		return "daxili xəta baş verdi"
	}
	return msg
}

// containsAny checks if a string contains any of the substrings (case-insensitive).
func containsAny(s string, substrs []string) bool {
	sLower := strings.ToLower(s)
	for _, sub := range substrs {
		if strings.Contains(sLower, sub) {
			return true
		}
	}
	return false
}
