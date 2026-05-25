// Package auth implements local authentication for Vantyx.
//
// It is intentionally small: a user store backed by SQLite, bcrypt
// password hashing with a complexity policy, a server-side session
// store keyed by random IDs, and SSH public key registration used by
// the CLI gateway.
//
// Cookies and HTTP-level concerns live in [internal/httpapi]; this
// package only deals with persistence and the cryptographic primitives.
package auth
