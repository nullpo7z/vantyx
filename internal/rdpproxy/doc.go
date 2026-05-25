// Package rdpproxy bridges a WebSocket client to an RDP server over TCP.
//
// It is a raw byte pump: each frame received over the WebSocket is
// forwarded verbatim to the remote TCP endpoint, and vice versa.
// Higher-level concerns (authentication, NLA, recording) are handled by
// the caller.
package rdpproxy
