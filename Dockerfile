# syntax=docker/dockerfile:1.7

FROM golang:1.24-alpine AS builder

WORKDIR /src

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    sh -c 'for i in 1 2 3; do go mod download && exit 0; echo "go mod download failed, retrying ($i/3)..." >&2; sleep 5; done; exit 1'

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/claude-server .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates wget && \
    addgroup -S claude && \
    adduser -S -G claude -h /app claude && \
    mkdir -p /app /data && \
    chown -R claude:claude /app /data

WORKDIR /app

COPY --from=builder /out/claude-server /app/claude-server

USER claude

EXPOSE 62311
VOLUME ["/data"]

ENTRYPOINT ["/app/claude-server"]
CMD ["-no-browser", "-data-dir=/data"]
