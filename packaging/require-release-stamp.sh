#!/usr/bin/env bash
# Refuse a shipped program that is not exactly the release it ships in
# (design-encrypted-archives-phase3-program §7, condition 12; SC33): its build
# information must name the tag as the main module's version and say the
# checkout was not modified. The program copies an encrypted archive keeps trust
# a release only when this holds.
#
# Usage: packaging/require-release-stamp.sh TAG FILE...
set -euo pipefail

tag="${1:?usage: require-release-stamp.sh TAG FILE...}"
shift
if ! [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "NOT A RELEASE TAG: $tag (a release is exactly vMAJOR.MINOR.PATCH)" >&2
  exit 1
fi
for f in "$@"; do
  info="$(go version -m "$f" 2>/dev/null || true)"
  main="$(awk '$1=="mod" {print $3; exit}' <<<"$info")"
  if [[ "$main" != "$tag" ]]; then
    echo "NOT STAMPED: $f names version '${main:-none}', not $tag (build from a clean checkout of the tag)" >&2
    exit 1
  fi
  if ! grep -q 'vcs.modified=false' <<<"$info"; then
    echo "NOT STAMPED: $f was built from a modified checkout (vcs.modified is not false)" >&2
    exit 1
  fi
done
