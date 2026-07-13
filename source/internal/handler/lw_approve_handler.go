package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"rdc-source/pkg/lw"
)

// approveLoanRequest is the request body for POST /api/lw/loans/approve.
// Uses flexInt so application_id accepts both int and string (Postman variables).
type approveLoanRequest struct {
	ApplicationID flexInt `json:"application_id"`
	Amount        float64 `json:"amount"`
	CardNumber    string  `json:"card_number"`
	CreditLevel   string  `json:"credit_level"`
	TermMonths    int     `json:"term_months"`
}

// ApproveLoan handles POST /api/lw/loans/approve
// Pushes an approved loan to LW for contract signing and money transfer.
func (h *LWRouterHandler) ApproveLoan(w http.ResponseWriter, r *http.Request) {
	var req approveLoanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLWRouterError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	appID := req.ApplicationID.Int()
	if appID <= 0 {
		writeLWRouterError(w, http.StatusBadRequest, "application_id must be a positive integer")
		return
	}

	lwReq := &lw.ApproveLoanRequest{
		ApplicationID: appID,
		Amount:        req.Amount,
		CardNumber:    req.CardNumber,
		CreditLevel:   req.CreditLevel,
		TermMonths:    req.TermMonths,
	}

	resp, err := h.lwProvider.ApproveLoan(r.Context(), lwReq)
	if err != nil {
		slog.Error("LW ApproveLoan failed", "application_id", appID, "error", err)
		writeLWRouterError(w, http.StatusBadGateway, "failed to approve loan on LW: "+err.Error())
		return
	}

	writeLWRouterJSON(w, http.StatusOK, resp)
}
