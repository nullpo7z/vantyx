// Package sqlite is the SQLite driver wrapper used by Vantyx.
//
// [Open] returns a configured [*sql.DB] handle backed by the
// modernc.org/sqlite pure-Go driver. The default file path is taken
// from [DefaultPath] (overridable via VANTYX_SQLITE_PATH). Foreign keys
// are enabled and the schema migrations defined in this package run
// automatically.
package sqlite
