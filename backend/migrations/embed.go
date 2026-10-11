// Package migrations embeds the Postgres (tern) and ClickHouse SQL migrations.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed postgres/*.sql clickhouse/*.sql
var files embed.FS

// Postgres returns the filesystem holding the tern Postgres migrations.
func Postgres() fs.FS {
	return mustSub("postgres")
}

// ClickHouse returns the filesystem holding the ClickHouse migrations.
func ClickHouse() fs.FS {
	return mustSub("clickhouse")
}

func mustSub(dir string) fs.FS {
	sub, err := fs.Sub(files, dir)
	if err != nil {
		// fs.Sub only fails for invalid names; callers pass constants.
		panic("migrations: " + err.Error())
	}

	return sub
}
