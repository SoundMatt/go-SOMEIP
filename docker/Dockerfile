# Multi-stage build for go-SOMEIP quickstart binaries.
# Both targets (server, client) are compiled from a single builder stage.

FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /bin/server ./examples/quickstart/server
RUN CGO_ENABLED=0 go build -trimpath -o /bin/client ./examples/quickstart/client

# ── server image ───────────────────────────────────────────────────────────────
FROM alpine:3.20 AS server
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/server /usr/local/bin/server
LABEL org.opencontainers.image.title="go-SOMEIP server" \
      org.opencontainers.image.source="https://github.com/SoundMatt/go-SOMEIP" \
      org.opencontainers.image.licenses="MPL-2.0"
EXPOSE 30509/udp
ENTRYPOINT ["server"]

# ── client image ───────────────────────────────────────────────────────────────
FROM alpine:3.20 AS client
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/client /usr/local/bin/client
LABEL org.opencontainers.image.title="go-SOMEIP client" \
      org.opencontainers.image.source="https://github.com/SoundMatt/go-SOMEIP" \
      org.opencontainers.image.licenses="MPL-2.0"
ENTRYPOINT ["client"]
