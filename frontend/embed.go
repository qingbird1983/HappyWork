// Package frontend embeds the built frontend assets (frontend/dist) so that
// cmd/agent can hand them to the desktop layer. The embed directive cannot
// reach a sibling directory, which is why this file lives here.
package frontend

import "embed"

//go:embed all:dist
var dist embed.FS

// Dist returns the embedded production frontend assets.
func Dist() embed.FS { return dist }
