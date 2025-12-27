# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/effectusd ./cmd/effectusd
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/effectusc ./cmd/effectusc

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && addgroup -S effectus && adduser -S effectus -G effectus

WORKDIR /app
COPY --from=builder /out/effectusd /usr/local/bin/effectusd
COPY --from=builder /out/effectusc /usr/local/bin/effectusc

USER effectus
ENTRYPOINT ["effectusd"]
