// Package proxyerrors defines the shared error vocabulary used by the
// protocol bridges (SSH, Telnet, VNC, RDP).
//
// [UserFacingError] wraps a low-level dial or I/O failure with a short
// hint that can be shown in the browser terminal. [WrapTCPDialError]
// classifies the most common TCP failure modes (timeout, refused, no
// route) and produces a [UserFacingError]; [BridgeErrorMessage] and
// [UnwrapForAudit] extract the user-facing text and the original cause
// for log entries.
package proxyerrors
