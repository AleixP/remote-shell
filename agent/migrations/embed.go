// Package migrations embeds the SQL schema migrations of the agent database.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
