FROM golang:1.25.13-alpine@sha256:1e0126852075c9c60731c8ba49088448b91f63e2aed97ca9d1a9791622a05946 AS gosu-builder

RUN apk add --no-cache git \
  && git clone --depth 1 --branch 1.19 https://github.com/tianon/gosu /tmp/gosu \
  && test "$(git -C /tmp/gosu rev-parse HEAD)" = "6456aaa0f3c854d199d0f037f068eb97515b7513" \
  && cd /tmp/gosu \
  && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gosu .

FROM mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b

COPY --from=gosu-builder /out/gosu /usr/local/bin/gosu
RUN microdnf update -y \
  && rm -rf /usr/bin/mysqlsh /usr/lib/mysqlsh \
  && microdnf clean all
