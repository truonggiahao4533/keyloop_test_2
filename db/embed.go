// Package db embeds the SQL migrations so the binary can apply them.
package db

import "embed"

// Migrations holds the goose migration files.
//
//go:embed migrations/*.sql
var Migrations embed.FS
