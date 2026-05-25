// Package rdpvnc owns the bookkeeping for the future RDP-to-VNC bridge
// and the live VNC session manager used by the HTTP API.
//
// [Manager] tracks each open VNC / RDP-VNC session, ferries the
// activity heartbeats that drive idle warnings, and exposes a JSON
// snapshot consumed by the sessions UI. The actual byte pump for VNC
// lives in [internal/vncproxy]; the RDP-side bridge is on the roadmap.
package rdpvnc
