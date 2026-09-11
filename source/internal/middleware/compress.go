package middleware

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

// PR #502: gzip compression middleware.
//
// Səbəb: /apply və /detail HTML-ləri ~140KB-dir və sıxılmamış göndərilirdi —
// "Kredit al" düyməsi ilə /apply-a keçid hiss olunacaq dərəcədə yavaş idi.
// Caddy-də encode direktivi yoxdur (Caddyfile istifadəçi tərəfindən idarə
// olunur), ona görə compression app tərəfdə edilir.
//
// Qaydalar:
//   - Yalnız client Accept-Encoding: gzip göndərəndə (bütün müasir brauzerlər)
//   - Yalnız compressable content-type-lar (text/html, json, js, css, svg...)
//   - POST/PUT sorğuların BODY-si sıxılmır — yalnız RESPONSE sıxılır
//   - gzip.Writer pool (sync.Pool) — alloc azaltmaq üçün
//
// HTML-lər Cache-Control: no-cache qalır — revalidasiya davranışı dəyişmir,
// sadəcə transfer ~10x kiçilir (140KB → ~15KB).

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		// DefaultCompression (-1) — sürət/nisbət balansı
		return gzip.NewWriter(nil)
	},
}

// compressibleContentType oyunca content-type prefiksleri.
// Başlanğıc status sual altında olan hallarda (set olunmamış) compress etmirik.
func compressibleContentType(ct string) bool {
	for _, prefix := range []string{
		"text/",
		"application/json",
		"application/javascript",
		"application/svg",
		"image/svg",
	} {
		if strings.HasPrefix(ct, prefix) {
			return true
		}
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	usedGzip    bool // WriteHeader compression seçibsə true — Close lazımdır
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	ct := g.Header().Get("Content-Type")
	if !compressibleContentType(ct) {
		// Sıxıla bilməz — adi cavab: gz istifadəsiz, Write birbaşa ResponseWriter-a gedir
		g.gz = nil
		g.ResponseWriter.WriteHeader(status)
		return
	}
	g.usedGzip = true
	g.Header().Set("Content-Encoding", "gzip")
	// Content-Length artıq yanlış olacaq — sil (chunked yazılacaq)
	g.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		// WriteHeader çağrılmayıbsa stdlib avtomatik 200 çağırır — bizim
		// WriteHeader-ın dolanışıq yolu ilə işə düşməsi üçün özümüz çağırırıq.
		g.WriteHeader(http.StatusOK)
	}
	if g.gz == nil {
		// compressable deyil — birbaşa yaz
		return g.ResponseWriter.Write(b)
	}
	return g.gz.Write(b)
}

// Flush — SSE/streaming üçün (KYC polling uzun sorğularında buffering olmasın).
func (g *gzipResponseWriter) Flush() {
	if g.gz != nil {
		g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Compress — response-u gzip ilə sıxır (Accept-Encoding olduqda).
func Compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			r.Header.Get("Range") != "" { // Range sorğularında gzip etmə
			next.ServeHTTP(w, r)
			return
		}

		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)
		gw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
		next.ServeHTTP(gw, r)

		// Compression istifadə olunubsa gzip trailer yazılmalıdır (Close).
		// İstifadə olunmayıbsa (məs. 204, incompressible type) Close ÇAĞRILMAMALI
		// — yoxsa boş gzip trailer-i response-a yapışdırar (PR #502 testi tapdı).
		if gw.usedGzip {
			gz.Close()
		}
		gzipWriterPool.Put(gz)
	})
}
