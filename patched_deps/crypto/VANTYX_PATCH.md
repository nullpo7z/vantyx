# Vantyx local patch of golang.org/x/crypto

This directory is upstream **golang.org/x/crypto v0.56.0** (wired in via the
`replace golang.org/x/crypto => ./patched_deps/crypto` directive in the
repository root `go.mod`) with **one** functional change:

- `ssh/keys.go`: decrypt OpenSSH private keys encrypted with
  `aes256-gcm@openssh.com` (in addition to upstream's `aes256-ctr` /
  `aes256-cbc`). Upstream still rejects that cipher. Search for
  `aes256-gcm@openssh.com` in the OpenSSH key-parsing switch.

Everything else is verbatim upstream. Files are `gofmt`-formatted because the
repository CI runs `gofmt -l .` over this tree as well (upstream ships one
unformatted testdata file).

## Why this matters for security

`govulncheck` cannot attribute a version to a directory replacement, so it
reports nothing for this module. The base version therefore has to be checked
by hand against the Go vulnerability database whenever the tree is touched:

    mkdir /tmp/vc && cd /tmp/vc && go mod init vc
    printf 'package main\nimport _ "golang.org/x/crypto/ssh"\nfunc main(){}\n' > main.go
    go get golang.org/x/crypto@v0.56.0 && go mod tidy
    go run golang.org/x/vuln/cmd/govulncheck@latest -scan module

History: the tree was first vendored from v0.48.0, which by 2026-09 carried 15
published `x/crypto/ssh` advisories (auth/certificate bypasses and DoS,
GO-2026-5013 … GO-2026-6355, all fixed by v0.52.0 – v0.56.0). It was rebased
to v0.56.0 on 2026-09-08. Do not let it fall behind again.

## Rebasing to a newer upstream

    v=v0.NN.0
    a=$(go mod download -json golang.org/x/crypto@v0.56.0 | jq -r .Dir)   # current base
    b=$(go mod download -json golang.org/x/crypto@$v      | jq -r .Dir)
    diff -u "$a/ssh/keys.go" patched_deps/crypto/ssh/keys.go > /tmp/keys.patch
    cp -r "$b" /tmp/new && chmod -R u+w /tmp/new
    patch /tmp/new/ssh/keys.go /tmp/keys.patch      # must apply cleanly
    gofmt -w /tmp/new
    rm -rf patched_deps/crypto && mv /tmp/new patched_deps/crypto
    go mod tidy && go test ./internal/sshproxy/ ./internal/sshd/ ./internal/httpapi/

Then update the base version at the top of this file.
