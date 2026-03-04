FROM golang:1.26 AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/vantyx ./cmd/vantyx-server

FROM gcr.io/distroless/base-debian12:nonroot

USER nonroot:nonroot
WORKDIR /app

COPY --from=builder /out/vantyx /app/vantyx

EXPOSE 8080

ENTRYPOINT ["/app/vantyx"]

