package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ErrVideoRecordNotFound — PR #427: müraciət tapıldı, amma video order yoxdur
// (müraciət üçün video record yaradılmayıb).
var ErrVideoRecordNotFound = fmt.Errorf("video record tapılmadı")

// ErrVideoNotRecorded — PR #427: video order var, amma müştəri hələ çəkməyib
// (recorded=false). Stream URL bu halda "ölü" olduğundan 404 qaytarılır —
// LW ölü link açmır, "video var amma çəkilməyib" məlumatı xaricə çıxmır.
var ErrVideoNotRecorded = fmt.Errorf("video hələ çəkilməyib")

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

// GetVideoStreamURLByPINAndDate — PR #427: LW partner endpoint-i üçün.
// PIN + müraciətin yaradıldığı gün (yyyy-mm-dd) ilə həmin günün ən son
// müraciətini tapır və video_records-dən stream URL-i qurur.
//
// Eyni PIN + eyni gündə bir neçə müraciət varsa ən yenisi qaytarılır
// (repo tərəfində ORDER BY id DESC). Hər çağırış Loki-yə loglanır (audit).
func (s *ApplicationService) GetVideoStreamURLByPINAndDate(ctx context.Context, pin, day string) (*PartnerVideoInfo, error) {
	if s.videoStreamBaseURL == "" {
		return nil, fmt.Errorf("video stream base URL konfiqurasiya olunmayıb")
	}

	app, err := s.repo.FindLatestByPINAndDate(ctx, pin, day)
	if err != nil {
		return nil, fmt.Errorf("failed to find application: %w", err)
	}
	if app == nil {
		return nil, ErrApplicationNotFound
	}

	vr, err := s.videoRecordRepo.GetByApplication(ctx, app.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get video record: %w", err)
	}
	if vr == nil {
		return nil, ErrVideoRecordNotFound
	}
	if !vr.Recorded {
		return nil, ErrVideoNotRecorded
	}

	info := &PartnerVideoInfo{
		AppID:                vr.AppIDExternal,
		StreamURL:            strings.TrimRight(s.videoStreamBaseURL, "/") + "/video/" + vr.AppIDExternal + "/stream",
		Recorded:             vr.Recorded,
		ApplicationCreatedAt: app.CreatedAt,
		VideoCreatedAt:       vr.CreatedAt,
		ApplicationID:        app.ID, // PR #431: audit üçün (JSON-da yox)
	}

	slog.Info("partner video url served",
		"pin", pin,
		"application_id", app.ID,
		"app_id_external", vr.AppIDExternal,
		"recorded", vr.Recorded)

	return info, nil
}

// GetVideoStreamURLByPIN — PR #432: PIN-lə axtarış (tarix YOX).
// Həmin PIN-in ÇƏKİLMİŞ (recorded=1) videolu ən son müraciətini tapır və
// stream URL qurur. Yeni müraciətlərdə video hələ çəkilməyibsə onlar ötürülür —
// LW "müştərinin videosunu göstər" sorğusunda ən son İZLƏNİLƏ BİLƏN video
// qayıdır (müraciətin hansı gündə yaradıldığını bilmək lazım deyil).
func (s *ApplicationService) GetVideoStreamURLByPIN(ctx context.Context, pin string) (*PartnerVideoInfo, error) {
	if s.videoStreamBaseURL == "" {
		return nil, fmt.Errorf("video stream base URL konfiqurasiya olunmayıb")
	}

	appID, err := s.repo.FindLatestAppIDByPINWithRecordedVideo(ctx, pin)
	if err != nil {
		return nil, fmt.Errorf("failed to find application by pin: %w", err)
	}
	if appID == 0 {
		return nil, ErrVideoRecordNotFound
	}

	app, err := s.repo.GetApplicationByID(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return nil, ErrApplicationNotFound
	}

	// PR #432: yalnız ÇƏKİLMİŞ videolar — ekspert yeni (çəkilməmiş) sifariş
	// göndəribsə köhnə çəkilmiş video itmir (GetLatestRecordedByApplication).
	vr, err := s.videoRecordRepo.GetLatestRecordedByApplication(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get video record: %w", err)
	}
	if vr == nil {
		return nil, ErrVideoRecordNotFound
	}

	info := &PartnerVideoInfo{
		AppID:                vr.AppIDExternal,
		StreamURL:            strings.TrimRight(s.videoStreamBaseURL, "/") + "/video/" + vr.AppIDExternal + "/stream",
		Recorded:             true, // GetLatestRecordedByApplication yalnız recorded=1 qaytarır
		ApplicationCreatedAt: app.CreatedAt,
		VideoCreatedAt:       vr.CreatedAt,
		ApplicationID:        app.ID, // PR #431: audit üçün (JSON-da yox)
	}

	slog.Info("partner video url served (pin-only)",
		"pin", pin,
		"application_id", app.ID,
		"app_id_external", vr.AppIDExternal)

	return info, nil
}
