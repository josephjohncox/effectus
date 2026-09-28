#!/bin/sh
# Frozen compat/v03 imports first shipped in v0.4.0.
set -eu

if [ "$#" -ne 1 ]; then
  echo 'usage: recovery-compat-policy.sh VERSION' >&2
  exit 2
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
version=$1
"$script_dir/validate-release-version.sh" "$version" >/dev/null

core=${version%%-*}
major=${core%%.*}
minor_and_patch=${core#*.}
minor=${minor_and_patch%%.*}
patch=${minor_and_patch#*.}

if [ "$major" = 0 ]; then
  case "$minor" in
    0|1|2|3)
      printf 'not-applicable\n'
      exit 0
      ;;
    4)
      # A v0.4.0 prerelease precedes the first published compatibility tag.
      if [ "$patch" = 0 ] && [ "$version" != "$core" ]; then
        printf 'not-applicable\n'
        exit 0
      fi
      ;;
  esac
fi

printf 'required\n'
