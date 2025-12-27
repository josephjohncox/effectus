FROM postgres:16

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates git build-essential postgresql-server-dev-16 \
  && update-ca-certificates \
  && git clone --depth 1 https://github.com/eulerto/wal2json /tmp/wal2json \
  && make -C /tmp/wal2json \
  && make -C /tmp/wal2json install \
  && rm -rf /tmp/wal2json \
  && apt-get purge -y --auto-remove git build-essential postgresql-server-dev-16 \
  && rm -rf /var/lib/apt/lists/*
