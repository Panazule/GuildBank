# syntax=docker/dockerfile:1.7

# Multi-stage build producing a static CGO-free binary (IP Phase 1 task 7).
ARG GO_VERSION=1.27
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
      -o /out/guildbank ./cmd/guildbank

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 guildbank \
    && adduser -S -D -H -u 10001 -G guildbank guildbank \
    && mkdir -p /app /config \
    && chown guildbank:guildbank /app /config

COPY --from=build /out/guildbank /app/guildbank

WORKDIR /app
USER 10001:10001
EXPOSE 8080
VOLUME ["/config"]

ENV GUILDBANK_PORT=8080 \
    GUILDBANK_SQLITE_PATH=/config/guildbank.db \
    GUILDBANK_LOG_LEVEL=info

# The liveness probe only proves the process responds; readiness is /readyz.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/guildbank"]