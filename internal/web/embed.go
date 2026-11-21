// Package web owns the server-side rendered assets. Phase 1 adds the setup,
// login and settings screens with embedded CSS and a small vanilla script.
// Phase 4+ adds inventory screens. All assets are embedded, never loaded from
// a CDN (IP Phase 1 task 6).
package web

import "embed"

// Templates holds the HTML templates rendered by the UI layer.
//
//go:embed templates/*.html
var Templates embed.FS

// Static holds CSS, JavaScript and vendored Bootstrap/HTMX assets.
//
//go:embed static/*
var Static embed.FS
