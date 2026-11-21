// Package web owns the server-side rendered assets. Phase 2 adds the setup
// and login templates; Phase 4+ adds inventory screens. Bootstrap 5 and HTMX
// are served from this embedded filesystem, never from a CDN (IP Phase 1
// task 6).
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
