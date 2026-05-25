// Package protocols centralises the capability matrix that says, for a
// given (protocol, feature) pair, whether the feature is supported.
//
// HTTP handlers branch on [SupportsTerminal], [SupportsFileTransfer],
// and [SupportsTFTPServer] rather than checking protocol literals so
// that adding a new bridge is a single change.
package protocols
