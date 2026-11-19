#!/usr/bin/env bash
# Run govulncheck as if github.com/ncruces/zenity v0.10.12 were the published module, not
# the copy in third_party/zenity: govulncheck cannot match a module replaced by a folder to
# its advisories, so this scan reads a go.mod without that replace
# (docs/design-windows-dialog-fit.md §7, MA-1468). It writes only to a temporary folder;
# the repository's go.mod and go.sum are not touched.
#
# Usage, from the repository root:
#   packaging/vulncheck-published-zenity.sh <path to govulncheck> [govulncheck arguments...]
# GOOS and the other go settings pass through, so the Windows build can be scanned too:
#   GOOS=windows packaging/vulncheck-published-zenity.sh "$(go env GOPATH)/bin/govulncheck" ./cmd/mailarchive-desktop
set -euo pipefail

govulncheck="${1:?usage: $0 <path to govulncheck> [govulncheck arguments...]}"
shift

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

go mod edit -dropreplace=github.com/ncruces/zenity@v0.10.12 -print > "$tmp/published.mod"
if grep -q 'third_party/zenity' "$tmp/published.mod"; then
  echo "the copy is still named in the scanned go.mod" >&2
  exit 1
fi
# go.sum, plus zenity's two lines as the pins recorded them; -mod=mod lets the go command
# fetch, and check against the Go checksum database, any other go.mod hash the published
# module's requirements need, into the temporary file only.
cp go.sum "$tmp/published.sum"
grep '^go.sum: ' internal/thirdparty/testdata/zenity.pins | sed 's/^go.sum: //' >> "$tmp/published.sum"

GOFLAGS="-modfile=$tmp/published.mod -mod=mod" "$govulncheck" "$@"
