package main

import "embed"

// webFiles holds the embedded frontend files (HTML, CSS, JS).
// The files are served from the web/ directory next to main.go.
//
//go:embed web
var webFiles embed.FS
