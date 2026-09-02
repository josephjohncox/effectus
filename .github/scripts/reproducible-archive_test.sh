#!/bin/sh
# Exercise the archive normalization used by publish and recovery workflows.
set -eu

temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
SOURCE_DATE_EPOCH=0
export SOURCE_DATE_EPOCH

make_archives() {
  name=$1
  stamp=$2
  work=$temp/$name/work
  mkdir -p "$work"
  printf 'effectusc\n' > "$work/effectusc"
  printf 'effectusd\n' > "$work/effectusd"
  touch -d "@$stamp" "$work"/*
  tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" \
    --owner=0 --group=0 --numeric-owner -C "$work" \
    -czf "$temp/$name.tar.gz" .
  (cd "$work" && touch -d "@$SOURCE_DATE_EPOCH" ./* && zip -X -q "$temp/$name.zip" ./*)
}

make_archives first 1
make_archives second 1700000000
cmp "$temp/first.tar.gz" "$temp/second.tar.gz"
cmp "$temp/first.zip" "$temp/second.zip"
