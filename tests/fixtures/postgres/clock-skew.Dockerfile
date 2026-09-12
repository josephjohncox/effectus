FROM postgres:16-alpine@sha256:cf78e76683b9ca8c5733cbbdce6c9262b45b6767934dd0a95e671f9a0fc20685 AS builder

RUN apk add --no-cache build-base curl
RUN curl --fail --location --silent --show-error --max-time 60 \
      https://codeload.github.com/wolfcw/libfaketime/tar.gz/86b37fde2fed7336ea2d0c17928e3015a55d9b4a \
      --output /tmp/libfaketime.tar.gz \
    && echo 'd98aa7beeb21344b3612f60cbfe08196541a2ef25525b3774b11871493a4726e  /tmp/libfaketime.tar.gz' | sha256sum -c - \
    && mkdir /build \
    && tar -xzf /tmp/libfaketime.tar.gz -C /build --strip-components=1 \
    && make -C /build \
    && make -C /build install

FROM postgres:16-alpine@sha256:cf78e76683b9ca8c5733cbbdce6c9262b45b6767934dd0a95e671f9a0fc20685
COPY --from=builder /usr/local/lib/faketime/libfaketimeMT.so.1 /usr/local/lib/faketime/libfaketimeMT.so.1
COPY clock-skew-entrypoint.sh /usr/local/bin/clock-skew-entrypoint.sh
USER postgres
ENTRYPOINT ["/bin/sh", "/usr/local/bin/clock-skew-entrypoint.sh"]
