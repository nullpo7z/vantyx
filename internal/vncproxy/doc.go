// Package vncproxy bridges a noVNC WebSocket client to a VNC server
// over TCP.
//
// It is a raw byte pump: each frame received over the WebSocket is
// forwarded verbatim to the RFB endpoint, and vice versa. An optional
// touch callback lets the caller update session last-seen timestamps
// for idle-warning bookkeeping.
package vncproxy
