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
# Stage 3: Runtime (Alpine edge for FreeRDP 3.x; gnome-remote-desktop on Ubuntu 24.04 requires it)
# -----------------------------------------------------------------------------
FROM alpine:edge

# su-exec, nonroot user, asciinema-agg (GIF 用), ffmpeg (WebM 用), フォント (agg の描画用)
# freerdp (3.x) + Xvfb + x11vnc: browser-based RDP via FreeRDP→Xvfb→x11vnc→noVNC
ARG AGG_VERSION=v1.7.0
RUN apk add --no-cache su-exec wget ffmpeg fontconfig font-dejavu \
	freerdp xvfb x11vnc xdpyinfo xkeyboard-config \
	&& adduser -D -u 65532 nonroot \
	&& wget -q "https://github.com/asciinema/agg/releases/download/${AGG_VERSION}/agg-x86_64-unknown-linux-musl" -O /usr/local/bin/agg \
	&& chmod +x /usr/local/bin/agg \
	&& fc-cache -f \
	&& apk del wget

WORKDIR /app

COPY --from=builder /out/vantyx /app/vantyx
COPY --from=frontend /src/web/dist /app/web/dist
COPY scripts/docker-entrypoint.sh /entrypoint.sh

RUN mkdir -p /app/certs /app/data && chown -R nonroot:nonroot /app/certs /app/data \
	&& chmod +x /entrypoint.sh

EXPOSE 80 443

ENTRYPOINT ["/entrypoint.sh"]
