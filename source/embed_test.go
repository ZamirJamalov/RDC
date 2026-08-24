package main

import (
	"io/fs"
	"strings"
	"testing"
)

// PR #293: executable-ın self-contained olduğunu təmin edən embed yoxlamaları —
// HTML/asset-lər (webFiles) və migration-lar (migrationFiles) binary-nin içindədir.

func TestWebFilesEmbedded(t *testing.T) {
	htmls, err := fs.Glob(webFiles, "web/*.html")
	if err != nil {
		t.Fatalf("glob web/*.html: %v", err)
	}
	if len(htmls) == 0 {
		t.Fatal("no html files embedded — web/ embed broken")
	}

	// Əsas səhifələr mövcud olmalıdır
	for _, want := range []string{
		"web/landing.html", "web/index.html", "web/login.html",
		"web/detail.html", "web/apply.html", "web/admin.html",
	} {
		found := false
		for _, h := range htmls {
			if h == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected embedded file %s missing", want)
		}
	}

	// Statik asset-lər (şəkillər) və JS
	for _, want := range []string{"web/assets/alpul-logo.png", "web/auth.js"} {
		if _, err := fs.ReadFile(webFiles, want); err != nil {
			t.Errorf("embedded asset %s missing: %v", want, err)
		}
	}
}

func TestMigrationsEmbedded(t *testing.T) {
	sqls, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations/*.sql: %v", err)
	}
	if len(sqls) == 0 {
		t.Fatal("no sql files embedded — migrations/ embed broken")
	}
	t.Logf("embedded migrations: %d files", len(sqls))

	// İlk migration mütləq mövcuddur
	found := false
	for _, s := range sqls {
		if s == "migrations/001_init.sql" {
			found = true
			break
		}
	}
	if !found {
		t.Error("migrations/001_init.sql not embedded")
	}

	// Hər faylın məzmunu boş olmamalıdır
	for _, s := range sqls {
		data, err := fs.ReadFile(migrationFiles, s)
		if err != nil {
			t.Errorf("read %s: %v", s, err)
			continue
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			t.Errorf("migration %s is empty", s)
		}
	}
}
