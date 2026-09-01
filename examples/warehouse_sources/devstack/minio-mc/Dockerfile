FROM golang:1.25.13-alpine@sha256:1e0126852075c9c60731c8ba49088448b91f63e2aed97ca9d1a9791622a05946 AS builder

RUN apk add --no-cache git \
  && git clone https://github.com/minio/mc /src/mc \
  && cd /src/mc \
  && git checkout 77f82e18b5401a65958f1619df6ebb994634bd88 \
  && test "$(git rev-parse HEAD)" = "77f82e18b5401a65958f1619df6ebb994634bd88" \
  && go get github.com/prometheus/prometheus@v0.311.3 \
       golang.org/x/crypto@v0.55.0 \
       golang.org/x/net@v0.57.0 \
       golang.org/x/text@v0.41.0 \
       google.golang.org/grpc@v1.82.1 \
  && go mod tidy \
  && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mc .

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce
RUN apk upgrade --no-cache \
  && addgroup -S minio \
  && adduser -S -G minio minio
COPY --from=builder /out/mc /usr/local/bin/mc
USER minio
ENTRYPOINT ["mc"]
