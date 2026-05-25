// Package sshd implements the CLI gateway: an SSH server that lets
// operators "ssh user@vantyx" and reach any target they can access.
//
// [Server] handles authentication (password and registered public
// keys), exposes a small text-mode menu (target list, connect,
// disconnect), and uses [internal/sshproxy] / [internal/telnetproxy]
// to bridge the user's PTY to the chosen target. When configured with
// a recordings directory it writes asciinema casts via
// [internal/recording].
package sshd
