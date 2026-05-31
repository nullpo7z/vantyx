# -----------------------------------------------------------------------------
# Stage 1: Frontend (Vite + Tailwind + xterm)
# -----------------------------------------------------------------------------
FROM node:26-alpine AS frontend

WORKDIR /src/web

COPY web/package.json web/package-lock.json* ./
RUN npm ci

COPY web/ ./
RUN npm run build

# -----------------------------------------------------------------------------
# Stage 2: Go binary
# -----------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
COPY patched_deps ./patched_deps
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/vantyx ./cmd/vantyx-server

# -----------------------------------------------------------------------------
# Stage 3: Runtime (Alpine 3.22 + FreeRDP 3.x community edge package).
# CWE-1104: pin to a non-rolling release tag so re-builds are
# reproducible and CVE auditing is meaningful. The `community-edge`
# overlay is only used to pull a FreeRDP 3.x build.
# -----------------------------------------------------------------------------
FROM alpine:3.23

# asciinema-agg (GIF 用), ffmpeg (WebM 用), フォント (agg の描画用)
# freerdp (3.x) + Xvfb + x11vnc: browser-based RDP via FreeRDP→Xvfb→x11vnc→noVNC
#
# 以前は `su-exec` で root → nonroot に降格していたが、`cap_drop: ALL` の
# 環境（Proxmox unprivileged LXC など）では setgroups() が EPERM になり
# 起動できないため、Docker の USER ディレクティブで最初から nonroot 起動
# する方式に切替（CWE-250 / 269: 不要権限の回避）。
ARG AGG_VERSION=v1.7.0
RUN apk add --no-cache wget ffmpeg fontconfig font-dejavu \
	freerdp xvfb x11vnc xdpyinfo xkeyboard-config tigervnc-client \
	&& adduser -D -u 65532 nonroot \
	&& wget -q "https://github.com/asciinema/agg/releases/download/${AGG_VERSION}/agg-x86_64-unknown-linux-musl" -O /usr/local/bin/agg \
	&& chmod +x /usr/local/bin/agg \
	&& fc-cache -f \
	&& apk del wget

WORKDIR /app

COPY --from=builder /out/vantyx /app/vantyx
COPY --from=frontend /src/web/dist /app/web/dist
COPY scripts/docker-entrypoint.sh /entrypoint.sh

RUN mkdir -p /app/certs /app/data /app/recordings \
	&& chown -R nonroot:nonroot /app/certs /app/data /app/recordings \
	&& chmod +x /entrypoint.sh

EXPOSE 80 443

USER nonroot:nonroot

ENTRYPOINT ["/entrypoint.sh"]
