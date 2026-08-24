package main

import "embed"

// migrationFiles holds the embedded SQL migrations (001_init.sql, ...).
// PR #293: migrations binary-nin İÇİNƏ embed olunur — executable faylı tək
// başına başqa serverə daşımaq kifayətdir, migrations/ qovluğunu yanında
// aparmaq lazım deyil. Yeni migration əlavə olunanda binary yenidən build
// olunmalıdır (go run / go build avtomatik yenidən compile edir).
//
//go:embed migrations
var migrationFiles embed.FS
