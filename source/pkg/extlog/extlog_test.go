package extlog

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMaskURL(t *testing.T) {
	in := "http://gw/send?user=u&password=Secret123&gsm=555"
	want := "http://gw/send?user=u&password=***&gsm=555"
	if got := mask(in); got != want {
		t.Errorf("mask url: got %s want %s", got, want)
	}
}

func TestMaskJSON(t *testing.T) {
	in := `{"username":"sa","password":"Abc123","apiKey":"k-42","pin":"1SBK08P"}`
	got := mask(in)
	if strings.Contains(got, "Abc123") || strings.Contains(got, "k-42") {
		t.Errorf("mask json leaked secret: %s", got)
	}
	if !strings.Contains(got, `"pin":"1SBK08P"`) {
		t.Errorf("mask json must not touch non-secret fields: %s", got)
	}
}

func TestTruncate(t *testing.T) {
	long := strings.Repeat("a", maxBody+100)
	got := truncate(long)
	if len(got) > maxBody+len("...(truncated)") {
		t.Errorf("truncate too long: %d", len(got))
	}
	// UTF-8 kəsilməsi: çoxbaytlı simvollarda düzgün sərhəd
	az := strings.Repeat("ə", 3000) // 2 bayt/simvol → 6000 bayt > maxBody
	gotAz := truncate(az)
	if !utf8.ValidString(gotAz) {
		t.Errorf("truncate produced invalid UTF-8")
	}
}

func TestShortUnchanged(t *testing.T) {
	in := `{"LoanData":{"amount":1}}`
	if got := truncate(in); got != in {
		t.Errorf("short string changed: %s", got)
	}
}
