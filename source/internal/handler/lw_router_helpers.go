package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// writeLWRouterJSON writes a JSON response with the given status code.
func writeLWRouterJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

// writeLWRouterError writes a JSON error response and logs the message.
func writeLWRouterError(w http.ResponseWriter, code int, message string) {
	slog.Warn("LW router error", "status_code", code, "message", message)
	writeLWRouterJSON(w, code, map[string]string{"error": message})
}
