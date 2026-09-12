# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
ARG SOURCE_DATE_EPOCH=0
FROM golang:1.26.8-alpine@sha256:ce864e7223ac17b1775e6fd0b4c0db580c2eb50e7953a427916379e4b92a1628 AS builder
ARG SOURCE_DATE_EPOCH

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o /out/effectusd ./cmd/effectusd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o /out/effectusc ./cmd/effectusc

# A scratch runtime has no mutable package-resolution step. The CA bundle is
# copied from the digest-pinned builder and is therefore part of the image
# provenance rather than resolved from an Alpine repository during release.
FROM scratch
ARG SOURCE_DATE_EPOCH
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/effectusd /usr/local/bin/effectusd
COPY --from=builder /out/effectusc /usr/local/bin/effectusc

ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENV TMPDIR=/data
WORKDIR /app
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/effectusd"]
