package service

import (
	"context"
	"errors"
	"testing"

	"rdc-source/internal/model"
	"rdc-source/pkg/azmk"
)

// PR #408: GetCustomerPhoto — AZMK PersonalInfo Image tagı (fail-soft).
func TestGetCustomerPhoto_FromProvider(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, CustomerPIN: "1ABC123", CustomerSerial: "AA1234567"}

	provider := &mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			Name:  "Sadiq",
			Image: "/9j/4AAQSkZJRgABAQEB", // base64 JPEG prefix (nümunə)
		},
	}
	svc := newAddressTestService(store)
	svc.SetCustomerDataProvider(provider)

	image, err := svc.GetCustomerPhoto(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if image != "/9j/4AAQSkZJRgABAQEB" {
		t.Fatalf("expected image from provider, got %q", image)
	}
	if provider.personalCalls != 1 {
		t.Fatalf("expected 1 personal info call, got %d", provider.personalCalls)
	}
}

// Şəkil yoxdursa (köhnə AZMK cavabları) boş string qaytarılır — xəta yox.
func TestGetCustomerPhoto_NoImage_Empty(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, CustomerPIN: "1ABC123"}

	provider := &mockAzmkCustomerData{personalData: &azmk.CustomerData{Name: "Sadiq"}} // Image boş
	svc := newAddressTestService(store)
	svc.SetCustomerDataProvider(provider)

	image, err := svc.GetCustomerPhoto(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if image != "" {
		t.Fatalf("expected empty image, got %q", image)
	}
}

// AZMK xətası → fail-soft: boş string, xəta yox (UI şəkli gizlədir).
func TestGetCustomerPhoto_AzmkError_FailSoft(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, CustomerPIN: "1ABC123"}

	provider := &mockAzmkCustomerData{personalErr: errors.New("AZMK unreachable")}
	svc := newAddressTestService(store)
	svc.SetCustomerDataProvider(provider)

	image, err := svc.GetCustomerPhoto(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected fail-soft success, got error: %v", err)
	}
	if image != "" {
		t.Fatalf("expected empty image on error, got %q", image)
	}
}

// Provider injeksiya olunmayıbsa (nil) — boş string, xəta yox.
func TestGetCustomerPhoto_NoProvider(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, CustomerPIN: "1ABC123"}
	svc := newAddressTestService(store) // customerDataProvider nil

	image, err := svc.GetCustomerPhoto(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected success (fail-soft), got error: %v", err)
	}
	if image != "" {
		t.Fatalf("expected empty image, got %q", image)
	}
}

// Yalnış / mövcud olmayan ID → xəta.
func TestGetCustomerPhoto_InvalidID(t *testing.T) {
	svc := newAddressTestService(newMockStore())
	if _, err := svc.GetCustomerPhoto(context.Background(), 0); err == nil {
		t.Fatal("expected error for invalid id")
	}
	if _, err := svc.GetCustomerPhoto(context.Background(), 999); err == nil {
		t.Fatal("expected error for missing application")
	}
}
