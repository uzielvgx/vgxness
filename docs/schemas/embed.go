// Package schemas owns the canonical VGXNESS JSON Schema resources.
package schemas

import "embed"

// Files contains the canonical schemas so installed binaries can validate
// contracts without depending on a repository checkout or network access.
//
//go:embed *.schema.json
var Files embed.FS
