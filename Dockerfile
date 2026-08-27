# syntax=docker/dockerfile:1
FROM golang:1.25.13-alpine@sha256:1e0126852075c9c60731c8ba49088448b91f63e2aed97ca9d1a9791622a05946 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/effectusd ./cmd/effectusd
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/effectusc ./cmd/effectusc

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce
RUN apk upgrade --no-cache \
  && apk add --no-cache ca-certificates \
  && addgroup -S -g 10001 effectus \
  && adduser -S -D -H -u 10001 -G effectus effectus

WORKDIR /app
COPY --from=builder /out/effectusd /usr/local/bin/effectusd
COPY --from=builder /out/effectusc /usr/local/bin/effectusc

USER 10001:10001
ENTRYPOINT ["effectusd"]
