# This folder: a copy of github.com/ncruces/zenity v0.10.12

**What it is.** The files of the published module `github.com/ncruces/zenity`
v0.10.12 (MIT, see LICENSE; copyright Nuno Cruces), copied on 2026-11-18 from the
Go module cache after `go mod verify` checked the cache against go.sum's line
`h1:o4SErDa0kQijlqG6W4OYYzO6kA0fGu34uegvJGcMLBI=`. The repository's go.mod keeps
`require github.com/ncruces/zenity v0.10.12` and adds

    replace github.com/ncruces/zenity v0.10.12 => ./third_party/zenity

so every import of `github.com/ncruces/zenity` builds this copy.

**Why.** MailArchive patches the library's Windows dialogs so they fit their words:
the published version draws its lists, prompts and buttons at fixed sizes and cuts
the rest with no sign (docs/design-windows-dialog-fit.md, §1).

**Left out** of the copy: every `_test.go`, `cmd/` (the library's command-line
program), `.github/` and `testdata/`. Nothing else.

**Patched files** (each listed in the pins with the SHA-256 of its upstream original and
of our version, the imports it adds, and the lab run it last passed; a change to one must
update its record in the same change): `list_windows.go`, `entry_windows.go` and
`progress_windows.go` (each measures its words and applies fit.go's layout, or keeps
upstream's when it cannot); `util_windows.go` (the measuring and placing helpers, at its
end); `internal/win/user32.go` and `internal/win/gdi32.go` (the new calls and constants);
and `internal/win/zsyscall_windows.go`, regenerated: upstream's generator
(`golang.org/x/sys/windows/mkwinsyscall` at x/sys v0.17.0, as this module's go.mod
pins it) reproduces upstream's file byte for byte (checked 2026-11-19), so the new calls
were added as `//sys` lines and the file regenerated; its diff against upstream only adds
lines. **Added files:** this `UPSTREAM.md`; `fit.go`, the layout arithmetic (pure, no
imports, built everywhere but called only from the Windows files); and `fit_test.go`,
its tests (MailArchive's MA-1469).

**How it is checked.** `internal/thirdparty/testdata/zenity.pins` lists all 131
files of the published module with their SHA-256 (copied, patched or left out); the
lines reproduce the module's `h1:` hash. MA-1468 (`internal/thirdparty`) holds the
copy to the pins on every test run and runs this module's own tests and `go vet`
(for Windows, macOS and Linux). The pins are written only by

    go test ./internal/thirdparty -run TestRecordZenityPins -zenity-record [-zenity-lab "<lab run>"]

and changing an upstream line is an operator decision. `go vet` has one known
finding here, upstream's, in the generated function `SHBrowseForFolder`
(`internal/win/zsyscall_windows.go`, "possible misuse of unsafe.Pointer"); MA-1468
allows exactly that one and fails if it changes. `.gitattributes` keeps a checkout
from rewriting a byte of this folder. This module keeps upstream's go.mod (go 1.20)
and go.sum, which are never patched; new code here follows Go 1.20's rules.

**Adding Windows calls.** `internal/win/zsyscall_windows.go` is generated from the
`//sys` lines. Before the first new call, check whether the generator version
upstream used reproduces the file byte for byte; if it does, add `//sys` lines and
regenerate; if not, add the stubs by hand in the file's `syscall.Syscall` style
beside matching `//sys` lines. The method used is recorded here, and the file's diff
against upstream only adds lines.

## Upkeep

The operator moves the copy. To move it to a new version:

1. Download the new version and check its `h1:` against the Go checksum database.
2. Land the untouched new version as its own commit first, so the patch stays a
   plain git diff.
3. Re-derive each patch file from its new upstream original, reading upstream's
   changes to it.
4. Run the recorder for the new pins (the constants in
   `internal/thirdparty/zenity_copy_test.go` name the version and its `h1:`).
5. Run MA-1470 (the Windows lab test) on the office test PC and record the result.
6. Read the design's §1 again: does the new version fix the sizes itself, and does
   its Linux list still change?

At each MailArchive minor release, read upstream's changes since v0.10.12 and record
each Windows change below as taken or left out, with the reason. MA-1470 is run
again after each Windows feature update, because the message box's layout is
Windows' own. If a public advisory names zenity, the govulncheck steps (which scan a
go.mod without the replace) report it: move to upstream's fixed release, or record
here why this copy is not affected.

Upstream changes, as of v0.10.15 (read 2026-11-18):

- `MB_SETFOREGROUND` on message boxes: left out. A dialog the person did not ask
  for never takes their keyboard (R39), and MailArchive brings its own dialogs
  forward itself (`internal/dialogfront`).
- `initFileTypes` end of string (`file_windows.go`): left out. MailArchive's one
  filter (`*.pst`, `*.ost`) fills 12 of the 16 places Go allocates, so a zero follows
  the string today; that count comes from how Go grows slices, so re-check it when a
  filter changes or the Go toolchain moves.
- `initFilters` (`file_windows.go:502`, the legacy dialog's filter string): left
  out. Only the legacy file dialog that zenity falls back to reads it.
- The multi-select loop fix: left out. MailArchive never selects several files.
- The regenerated `zsyscall_windows.go` (`syscall.Syscall` became `SyscallN`): left
  out. The same calls; taking it would bury the patch's additions in a whole-file
  diff.

## The way back

To return to upstream's dialogs while keeping every gate green: copy upstream's
files back over every patched file, mark them copied again in the pins, keep
`fit.go` and its tests, and ship that as the next patch release. Removing this
folder altogether (with the replace, MA-1468, MA-1469 and S86, and the govulncheck
steps) is a gate reduction, the operator's to accept when it is proposed.
