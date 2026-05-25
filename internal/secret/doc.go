// Package secret provides AES-256-GCM encryption for sensitive fields
// (currently stored SSH passwords) and the key-loading helpers used by
// callers to fetch the master key from the environment.
//
// Ciphertext values are prefixed with [CiphertextVersionPrefix] so
// future migrations can identify legacy entries. The key length must
// match [KeySize]; mismatched-length keys are rejected at load time so
// errors surface before any data is written.
//
// Operational guidance lives in docs/configuration.md
// (`VANTYX_SSH_PASSWORD_ENCRYPTION_KEY`).
package secret
