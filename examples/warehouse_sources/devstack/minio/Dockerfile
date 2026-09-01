FROM golang:1.25.13-alpine@sha256:1e0126852075c9c60731c8ba49088448b91f63e2aed97ca9d1a9791622a05946 AS builder

RUN apk add --no-cache git \
  && git clone https://github.com/minio/minio /src/minio \
  && cd /src/minio \
  && git checkout 7aac2a2c5b7c882e68c1ce017d8256be2feea27f \
  && test "$(git rev-parse HEAD)" = "7aac2a2c5b7c882e68c1ce017d8256be2feea27f" \
  && go get github.com/apache/thrift@v0.23.0 \
       github.com/buger/jsonparser@v1.1.2 \
       github.com/go-jose/go-jose/v4@v4.1.4 \
       github.com/prometheus/prometheus@v0.311.3 \
       go.opentelemetry.io/otel/sdk@v1.43.0 \
       golang.org/x/crypto@v0.55.0 \
       golang.org/x/net@v0.57.0 \
       golang.org/x/text@v0.41.0 \
       google.golang.org/grpc@v1.82.1 \
  && go mod tidy -e \
  && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/minio .

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce
RUN apk upgrade --no-cache \
  && addgroup -S minio \
  && adduser -S -G minio minio \
  && mkdir -p /data \
  && chown minio:minio /data
COPY --from=builder /out/minio /usr/local/bin/minio
USER minio
ENTRYPOINT ["minio"]
