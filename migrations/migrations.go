package migrations

import "embed"

// Files is the immutable, versioned application schema.
//
//go:embed *.sql
var Files embed.FS
