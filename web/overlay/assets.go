package overlay

import "embed"

// Dist contains the production overlay bundle built by Vite.
//
//go:embed all:dist
var Dist embed.FS
