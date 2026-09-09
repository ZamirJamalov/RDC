package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ErrVideoRecordNotFound — PR #427: müraciət tapıldı, amma ÇƏKİLMİŞ video yoxdur
// (recorded=1 sətri yoxdur). PR #435: köhnə "video order yoxdur" halı da buna
// düşür — partner cavabında fərq etmək lazım deyil, LW üçün nəticə eynidir: 404.
var ErrVideoRecordNotFound = fmt.Errorf("çəkilmiş video tapılmadı")

// PartnerVideoInfo — PR #427: LW partner endpoint-inin cavabı.
type PartnerVideoInfo struct {
	AppID                string    `json:"app_id"`                 // public_id (UUID) — video service-ə göndərilən app_id
	StreamURL            string    `json:"stream_url"`             // {VIDEO_URL}/video/{public_id}/stream
	Recorded             bool      `json:"recorded"`               // video çəkilibmi (status check-dən)
	ApplicationCreatedAt string    `json:"application_created_at"` // müraciətin yaradılma vaxtı (DB string format)
	VideoCreatedAt       time.Time `json:"video_created_at"`       // video order-in yaradılma vaxtı

	// ApplicationID — daxili INT id (PR #431: yalnız audit üçün, cavabda GÖRÜNMÜR).
	ApplicationID int `json:"-"`
}

// ListVideoStreamURLsByAzmkLoanID — PR #435: LW partner endpoint-i üçün.
// Kredit müqavilə nömrəsi (azmk_loan_id — GET /application/{id}/status
// cavabından gələn loanId, məs. "HO0030210") ilə müraciəti tapır və həmin
// müraciətin ÇƏKİLMİŞ (recorded=1) videolarının hamısını qaytarır
// (yeni → köhnə). Bir müqavilə bir müraciətə uyğun gəlir; ekspert təkrar
// video sifarişi göndəribsə belə köhnə çəkilmiş videolar itmir.
//
// Əvvəlki PIN / PIN+tarix axtarışları (PR #427/#432/#433) bu PR ilə silindi —
// LW artıq kredit müqavilə nömrəsi ilə sorğur.
func (s *ApplicationService) ListVideoStreamURLsByAzmkLoanID(ctx context.Context, azmkLoanID string) ([]*PartnerVideoInfo, error) {
	if s.videoStreamBaseURL == "" {
		return nil, fmt.Errorf("video stream base URL konfiqurasiya olunmayıb")
	}

	appID, err := s.repo.FindAppIDByAzmkLoanID(ctx, azmkLoanID)
	if err != nil {
		return nil, fmt.Errorf("failed to find application by azmk_loan_id: %w", err)
	}
	if appID == 0 {
		return nil, ErrApplicationNotFound
	}

	app, err := s.repo.GetApplicationByID(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get application %d: %w", appID, err)
	}
	if app == nil {
		return nil, ErrApplicationNotFound // defensive — silinmiş sətir
	}

	vrs, err := s.videoRecordRepo.ListRecordedByApplication(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to list video records %d: %w", appID, err)
	}
	if len(vrs) == 0 {
		return nil, ErrVideoRecordNotFound
	}

	base := strings.TrimRight(s.videoStreamBaseURL, "/")
	videos := make([]*PartnerVideoInfo, 0, len(vrs))
	for i := range vrs {
		vr := &vrs[i]
		videos = append(videos, &PartnerVideoInfo{
			AppID:                vr.AppIDExternal,
			StreamURL:            base + "/video/" + vr.AppIDExternal + "/stream",
			Recorded:             true, // ListRecordedByApplication yalnız recorded=1 qaytarır
			ApplicationCreatedAt: app.CreatedAt,
			VideoCreatedAt:       vr.CreatedAt,
			ApplicationID:        app.ID, // PR #431: audit üçün (JSON-da yox)
		})
	}

	slog.Info("partner video urls served (by azmk_loan_id)",
		"azmk_loan_id", azmkLoanID,
		"application_id", app.ID,
		"videos", len(videos))

	return videos, nil
}
