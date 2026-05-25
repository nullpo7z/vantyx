// Package tftp hosts both the TFTP client used to talk to remote TFTP
// servers and the embedded TFTP server used to provision network
// equipment that pulls firmware over TFTP.
//
// The embedded server is controlled by [Controller], which is wired
// from [cmd/vantyx-server] on startup. It listens on
// VANTYX_TFTP_LISTEN (defaults to :6969 because the container runs as
// non-root) and serves files under VANTYX_TFTP_ROOT. The HTTP API
// auto-registers a hidden "TFTP" target per real target so file
// transfers funnel through a single subtree.
package tftp
