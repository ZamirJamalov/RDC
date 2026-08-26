package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"rdc-source/internal/model"
	"rdc-source/pkg/azmk"
)

// PR #313: müştərinin AZMK-da qeydiyyatda olan kartları (apply səhifəsi).
//
// Axın:
//   - Müştəri apply addımında (pending_customer) kart daxil edərkən backend
//     yoxlayır: bu PIN ilə əvvəlki müraciətlərdən birində kart qeyd edilibmi?
//     (loan_applications.card_id doludur)
//   - Edilibsə → AZMK GET /card/{partner_id} ilə partnerin kartları gətirilir
//     və müştəriyə SEÇİM göstərilir: köhnə kartlardan biri YA yeni kart.
//   - Köhnə kart seçilərsə confirm-də selected_card_id göndərilir, AZMK
//     RegisterCard ÇAĞIRILMIR (kart artıq qeydiyyatdadır), card_id birbaşa
//     yazılır və disburse bu kart üzərindən gedir.
//
// Bütün xətalar fail-soft-dur: siyahı boş qaytarılır, müştəri yeni kart
// daxil edir (klassik axın qalır).

// ErrApplicationNotFound is returned when no application matches the public_id.
var ErrApplicationNotFound = errors.New("müraciət tapılmadı")

// CustomerCard is the UI-facing copy of an AZMK card entry.
// Yalnız maskalı kod daşıyır — tam PAN heç vaxt mövcud deyil.
type CustomerCard struct {
	ID       string `json:"id"`       // AZMK card ID (disburse-də istifadə olunur)
	Code     string `json:"code"`     // maskalı: "****-****-****-5559"
	Expiring string `json:"expiring"` // "2030-01-01"
}

// GetCustomerCardsByPublicID lists the customer's previously registered AZMK
// cards for the application with the given public_id (PR #313).
//
// Boş siyahı = müştərinin seçilə biləcəyi köhnə kart yoxdur (UI yalnız
// yeni-kart daxiletmə göstərir). Xətalar fail-soft: log + boş siyahı.
func (s *ApplicationService) GetCustomerCardsByPublicID(ctx context.Context, publicID string) ([]CustomerCard, error) {
	app, err := s.repo.GetApplicationByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrApplicationNotFound
		}
		return nil, fmt.Errorf("failed to fetch application: %w", err)
	}
	if app == nil {
		return nil, ErrApplicationNotFound
	}
	if app.Status != model.StatusPendingCustomer {
		return nil, fmt.Errorf("kart siyahısı yalnız təsdiq gözləyən müraciətlər üçün əlçatandır (cari status: %s)", app.Status)
	}

	// AZMK deaktivdir / partner hələ qeydiyyatdan keçməyib → köhnə kart yoxdur.
	if s.azmkProvider == nil || app.PartnerID == "" {
		return []CustomerCard{}, nil
	}

	// Ön-şərt (PR #243 ruhu — lazımsız AZMK sorğularından qaçın): bu müştəri
	// heç vaxt kart qeyd etməyibsə AZMK çağırmağa ehtiyac yoxdur.
	hasCard, err := s.repo.HasRegisteredCard(ctx, app.CustomerPIN, app.ID)
	if err != nil {
		// Fail-soft: xəta olanda yenə də AZMK sorğuya ehtiyac yoxdur.
		slog.Warn("PR #313: HasRegisteredCard failed — returning empty card list",
			"application_id", app.ID, "error", err)
		return []CustomerCard{}, nil
	}
	if !hasCard {
		return []CustomerCard{}, nil
	}

	cards, err := s.azmkProvider.GetCards(ctx, app.PartnerID)
	if err != nil {
		// Fail-soft: AZMK alınmadı → müştəri yeni kart daxil edəcək.
		slog.Warn("PR #313: AZMK GetCards failed — returning empty card list (customer will enter a new card)",
			"application_id", app.ID,
			"partner_id", app.PartnerID,
			"error", err)
		return []CustomerCard{}, nil
	}

	out := make([]CustomerCard, 0, len(cards))
	for _, c := range cards {
		if isCardExpired(c.Expiring, time.Now()) {
			continue // bitmiş kart seçilə bilməz
		}
		out = append(out, CustomerCard{ID: c.ID, Code: c.Code, Expiring: c.Expiring})
	}
	slog.Info("PR #313: saved cards listed",
		"application_id", app.ID,
		"partner_id", app.PartnerID,
		"total", len(cards),
		"active", len(out))
	return out, nil
}

// findActiveCardByID returns the card with the given ID from the list,
// skipping expired cards. Returns nil if not found (PR #313).
func findActiveCardByID(cards []azmk.CardInfo, id string, now time.Time) *azmk.CardInfo {
	for i := range cards {
		if cards[i].ID == id && !isCardExpired(cards[i].Expiring, now) {
			return &cards[i]
		}
	}
	return nil
}

// isCardExpired parses the AZMK "expiring" date ("2030-01-01") and reports
// whether the card has already expired. Bitmə gününün ÖZÜNDƏ kart hələ
// etibarlı sayılır; parse xətası olanda false (etibarlı sayılır).
func isCardExpired(expiring string, now time.Time) bool {
	t, err := time.ParseInLocation("2006-01-02", expiring, now.Location())
	if err != nil {
		return false
	}
	endOfExpiryDay := t.AddDate(0, 0, 1) // bitmə günü sonuna qədər etibarlı
	return endOfExpiryDay.Before(now)
}

// maskCardCode converts the AZMK masked code ("****-****-****-5559", 19
// chars) into the 16-char form that fits the card_number VARCHAR(16) column:
// "************5559" (PR #313).
func maskCardCode(code string) string {
	noDash := strings.ReplaceAll(code, "-", "")
	if len(noDash) <= 16 {
		return noDash
	}
	return noDash[len(noDash)-16:]
}
