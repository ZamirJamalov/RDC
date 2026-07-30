package handler

import (
        "database/sql"
        "encoding/json"
        "log/slog"
        "net/http"
        "strconv"
        "strings"

        "rdc-source/internal/model"
        "rdc-source/internal/service"
)

// DiscountCodeHandler handles HTTP requests for discount code operations.
//
// PR #96: public-facing endpoints for discount code validation.
//
// Endpoints:
//   - GET /api/discount-codes/validate?code=ALPUL-XXXXXX
//     Public endpoint used by apply.html for real-time validation.
//     Checks: code exists + status='active'. Does NOT check self-use
//     (the customer's PIN is not known at this point — that check
//     happens on customer-confirm). Returns discount_type + discount_value
//     so the frontend can show a preview to the customer.
type DiscountCodeHandler struct {
        discountSvc *service.DiscountCodeService
}

// NewDiscountCodeHandler creates a new DiscountCodeHandler.
func NewDiscountCodeHandler(discountSvc *service.DiscountCodeService) *DiscountCodeHandler {
        return &DiscountCodeHandler{discountSvc: discountSvc}
}

// validateDiscountCodeResponse is the JSON response for GET /api/discount-codes/validate.
type validateDiscountCodeResponse struct {
        Valid         bool    `json:"valid"`
        DiscountType  string  `json:"discount_type,omitempty"`
        DiscountValue float64 `json:"discount_value,omitempty"`
        PreviewText   string  `json:"preview_text,omitempty"`
}

// validateDiscountCodeErrorResponse is the JSON error response.
type validateDiscountCodeErrorResponse struct {
        Valid  bool   `json:"valid"`
        Reason string `json:"reason"`
}

// Validate handles GET /api/discount-codes/validate?code=ALPUL-XXXXXX
//
// Query params:
//   - code (required): the discount code to validate
//
// Response (200 OK, valid code):
//
//      {
//        "valid": true,
//        "discount_type": "percent",
//        "discount_value": 2.00,
//        "preview_text": "komissiyadan 2% endirim"
//      }
//
// Response (200 OK, invalid code):
//
//      {
//        "valid": false,
//        "reason": "not_found" | "already_used" | "expired"
//      }
//
// Note: This endpoint does NOT check self-use prevention. A customer can
// validate their own code here (it will show as 'valid'), but the actual
// redemption will be rejected by customer-confirm. This is intentional —
// the frontend doesn't know the customer's ID at this point.
func (h *DiscountCodeHandler) Validate(w http.ResponseWriter, r *http.Request) {
        code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
        if code == "" {
                writeDiscountJSON(w, http.StatusBadRequest, validateDiscountCodeErrorResponse{
                        Valid:  false,
                        Reason: "missing_code",
                })
                return
        }

        if h.discountSvc == nil {
                slog.Warn("discount-codes/validate: discountSvc is nil (service not wired)")
                writeDiscountJSON(w, http.StatusServiceUnavailable, validateDiscountCodeErrorResponse{
                        Valid:  false,
                        Reason: "service_unavailable",
                })
                return
        }

        // Fetch the code from the repo (we need the discount_type/value for preview).
        // We use a customerID of 0 for validation here — this bypasses the self-use
        // check (since 0 will never match any real customer ID). The active/expiry
        // checks still apply.
        dc, err := h.discountSvc.ValidateForCustomer(r.Context(), code, 0)
        if err != nil {
                // Determine the reason for the failure
                reason := "invalid"
                if strings.Contains(err.Error(), "istifadə olunub") {
                        reason = "already_used"
                } else if strings.Contains(err.Error(), "müddəti bitib") {
                        reason = "expired"
                } else if strings.Contains(err.Error(), "yanlış") {
                        reason = "not_found"
                } else if strings.Contains(err.Error(), "öz endirim") {
                        // Self-use — should not happen with customerID=0, but handle defensively
                        reason = "self_use"
                }
                // Note: err could be sql.ErrNoRows wrapped — that's 'not_found'
                if err == sql.ErrNoRows {
                        reason = "not_found"
                }
                slog.Info("discount-codes/validate: code rejected",
                        "code", code,
                        "reason", reason)
                writeDiscountJSON(w, http.StatusOK, validateDiscountCodeErrorResponse{
                        Valid:  false,
                        Reason: reason,
                })
                return
        }

        // Build preview text
        previewText := ""
        switch dc.DiscountType {
        case model.DiscountTypePercent:
                previewText = "komissiyadan " + formatFloat(dc.DiscountValue) + "% endirim"
        case model.DiscountTypeFixed:
                previewText = formatFloat(dc.DiscountValue) + " AZN endirim"
        default:
                previewText = "endirim tətbiq olunacaq"
        }

        slog.Info("discount-codes/validate: code is valid",
                "code", code,
                "discount_type", dc.DiscountType,
                "discount_value", dc.DiscountValue)

        writeDiscountJSON(w, http.StatusOK, validateDiscountCodeResponse{
                Valid:         true,
                DiscountType:  dc.DiscountType,
                DiscountValue: dc.DiscountValue,
                PreviewText:   previewText,
        })
}

// --- Helpers ---

func writeDiscountJSON(w http.ResponseWriter, code int, data interface{}) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(code)
        json.NewEncoder(w).Encode(data)
}

// formatFloat formats a float64 for display, stripping trailing zeros
// (e.g. 2.00 → "2", 2.50 → "2.5", 2.25 → "2.25").
func formatFloat(f float64) string {
        s := strconv.FormatFloat(f, 'f', 2, 64)
        // Strip trailing ".00"
        if strings.HasSuffix(s, ".00") {
                s = strings.TrimSuffix(s, ".00")
        } else if strings.HasSuffix(s, "0") {
                // Strip single trailing zero (e.g. "2.50" → "2.5")
                s = s[:len(s)-1]
        }
        return s
}
