package resources

import "embed"

// FS is an embedded filesystem.
//
//go:embed node_modules
var FS embed.FS
