FROM golang:1.25.13-alpine@sha256:1e0126852075c9c60731c8ba49088448b91f63e2aed97ca9d1a9791622a05946 AS gosu-builder

RUN apk add --no-cache git \
  && git clone --depth 1 --branch 1.19 https://github.com/tianon/gosu /tmp/gosu \
  && test "$(git -C /tmp/gosu rev-parse HEAD)" = "6456aaa0f3c854d199d0f037f068eb97515b7513" \
  && cd /tmp/gosu \
  && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gosu .

FROM postgres:16-alpine@sha256:cf78e76683b9ca8c5733cbbdce6c9262b45b6767934dd0a95e671f9a0fc20685

COPY --from=gosu-builder /out/gosu /usr/local/bin/gosu

RUN apk upgrade --no-cache \
  && apk add --no-cache --virtual .wal2json-build build-base ca-certificates clang21 git llvm21 postgresql-dev \
  && git clone --depth 1 --branch wal2json_2_6 https://github.com/eulerto/wal2json /tmp/wal2json \
  && test "$(git -C /tmp/wal2json rev-parse HEAD)" = "75629c2e1e81a12350cc9d63782fc53252185d8d" \
  && make -C /tmp/wal2json \
  && make -C /tmp/wal2json install \
  && rm -rf /tmp/wal2json \
  && apk del .wal2json-build
