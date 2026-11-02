// Package migrations embeds the SQL schema migrations, applied in version order by golang-migrate.
package migrations

import "embed"

// FS holds every migration as <version>_<name>.{up,down}.sql.
//
//go:embed *.sql
var FS embed.FS
