package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// flexInt is a custom type that can be unmarshaled from either a JSON number
// or a JSON string containing a number. This is needed because Postman
// environment variables are always strings — when a request body contains
// {{application_id}}, it becomes a string in the JSON even if the Go struct
// expects an int.
//
// Example JSON that works:
//
//	{"application_id": 1}           // number → OK
//	{"application_id": "1"}         // string → OK (Postman variable)
//	{"application_id": "abc"}       // invalid → error
type flexInt int

// UnmarshalJSON implements json.Unmarshaler for flexInt.
func (f *flexInt) UnmarshalJSON(data []byte) error {
	// Try as number first
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexInt(n)
		return nil
	}

	// Try as string
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s == "" {
			return fmt.Errorf("empty string is not a valid integer")
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("cannot parse %q as integer: %w", s, err)
		}
		*f = flexInt(n)
		return nil
	}

	return fmt.Errorf("value must be a number or a numeric string")
}

// Int returns the underlying int value.
func (f flexInt) Int() int { return int(f) }
