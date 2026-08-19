// Package web embeds the static single-page frontend served by the local
// HTTP server — plain HTML/CSS/JS, no build step, no framework.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var raw embed.FS

// Static is the embedded frontend, rooted at its own top level so it can
// be served directly from "/".
var Static fs.FS

func init() {
	sub, err := fs.Sub(raw, "static")
	if err != nil {
		panic(err) // embed misconfiguration is a build-time bug, not a runtime one
	}
	Static = sub
}
