// Package mock provides lightweight in-memory servers used by tests.
//
// It currently exposes echo-style SSH and Telnet servers that the
// proxy and HTTP test suites use to exercise the bridge paths without
// requiring a real network or container.
package mock
