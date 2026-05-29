// Package secret provides AES-256-GCM encryption for sensitive fields at rest.
//
// Ciphertext values use the v2 prefix ([CiphertextVersionPrefixV2]) with an
// embedded key fingerprint and optional AAD binding per row/column.
package secret
