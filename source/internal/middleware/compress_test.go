package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// PR #502: gzip compression middleware testləri.

func TestCompress_HTMLGzipped(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(strings.Repeat("ALPUL ", 5000))) // ~30KB təkrarlı mətn — yaxşı sıxılır
	})

	req := httptest.NewRequest("GET", "/apply", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	Compress(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", ce)
	}

	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("response is not valid gzip: %v", err)
	}
	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("gzip read failed: %v", err)
	}
	if len(body) != 30000 { // 6 simvol × 5000
		t.Fatalf("decompressed length = %d, want 30000", len(body))
	}
	if rec.Body.Len() >= len(body) {
		t.Fatalf("gzip did not compress: %d bytes vs %d", rec.Body.Len(), len(body))
	}
}

func TestCompress_NoAcceptEncoding_Passthrough(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("salam"))
	})

	req := httptest.NewRequest("GET", "/", nil) // Accept-Encoding yoxdur
	rec := httptest.NewRecorder()
	Compress(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("must not set Content-Encoding without Accept-Encoding: gzip")
	}
	if rec.Body.String() != "salam" {
		t.Fatalf("body = %q, want raw", rec.Body.String())
	}
}

func TestCompress_IncompressibleType_Passthrough(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpegbytes"))
	})

	req := httptest.NewRequest("GET", "/photo.jpg", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	Compress(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("jpeg must not be gzipped")
	}
	if rec.Body.String() != "jpegbytes" {
		t.Fatalf("body = %q, want raw passthrough", rec.Body.String())
	}
}

func TestCompress_StatusNoContent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // 204 — body yoxdur
	})

	req := httptest.NewRequest("GET", "/api/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	Compress(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}
