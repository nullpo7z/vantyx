// Package db hosts database driver wrappers and schema migrations.
//
// The SQLite driver lives in the [sqlite] subpackage, which exposes the
// canonical [*sql.DB] handle used throughout the codebase. Migrations
// run automatically on Open so callers never observe a partially
// initialised schema.
package db
