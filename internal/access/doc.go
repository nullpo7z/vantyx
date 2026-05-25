// Package access owns Vantyx's authorisation model: targets, access
// groups, tags, and the relations that link them to users.
//
// It exposes:
//
//   - The domain types [Target], [TargetID], [Protocol], [Group], and
//     [GroupID] used by the HTTP API and the CLI gateway.
//   - The [TargetStore], [GroupStore], and [TagStore] interfaces that
//     describe the persistence contract, plus their SQLite-backed
//     implementations.
//   - Helpers such as [SQLiteAccessGroupStore.TargetIDsForUser] that the
//     handlers in internal/httpapi use to enforce per-user access.
//
// SQL queries always use placeholders, list calls accept a configurable
// timeout and page-size limit, and sensitive credential fields are
// encrypted via [internal/secret] before being persisted.
package access
