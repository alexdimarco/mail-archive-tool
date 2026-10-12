package util

// The shared primitives of design-mime-fidelity MC35. Each rule lives here and every
// site calls it, so two copies cannot drift: the PST reader and the raw-MIME body
// decoder share one legacy-text step, the mbox postmark and the page's sender share
// one lenient address extraction, and extract, verify and the repair's old-zip reader
// share one archive-file gate.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// DecodeLegacy turns the bytes of a text part into valid UTF-8: the bytes as they are
// when they already are, else decoded as Windows-1252. Windows-1252 is the guess for
// unlabelled or mislabelled western legacy text (a Cyrillic KOI8 message without a label
// is shown as mojibake, as the PST reader always has); it never fails and never reads
// a table that something else can change. An empty input is "".
func DecodeLegacy(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	if s, err := charmap.Windows1252.NewDecoder().String(string(b)); err == nil {
		return s
	}
	return string(b)
}

// maxLenientRunes caps what LenientAddress returns: a sender is shown on a page and in
// an index, never a megabyte of header.
const maxLenientRunes = 1000

// LenientAddress extracts a display name and an address from a From value that
// net/mail rejected (a bounce's "<>", "Mueller, Hans <h@x>", "Mail Delivery System
// <MAILER-DAEMON>", a name that is itself an address, a carrier's "a..b@x"). raw is
// already decoded text. The address is the contents of the last <…>, or the whole value
// when it has an "@" and no whitespace; the name is the text with that <…> removed and
// its quotes trimmed ("<>" alone gives the name "<>" and no address). Both are
// control-stripped and capped at 1,000 runes, and neither is ever an error: a value
// with no address is a name alone. An address with whitespace in it is not one.
func LenientAddress(raw string) (name, email string) {
	text := strings.TrimSpace(StripControl(raw))
	if text == "" {
		return "", ""
	}
	if lt := strings.LastIndexByte(text, '<'); lt >= 0 {
		if gt := strings.IndexByte(text[lt+1:], '>'); gt >= 0 {
			inner := strings.TrimSpace(text[lt+1 : lt+1+gt])
			if inner != "" && !strings.ContainsAny(inner, " \t") {
				email = inner
			}
			name = strings.Trim(strings.TrimSpace(text[:lt]+text[lt+1+gt+1:]), `" `)
			if name == "" && inner == "" {
				name = "<>"
			}
			return capRunes(name, maxLenientRunes), capRunes(email, maxLenientRunes)
		}
	}
	if strings.Contains(text, "@") && !strings.ContainsAny(text, " \t") {
		email = text
	}
	return capRunes(strings.Trim(text, `" `), maxLenientRunes), capRunes(email, maxLenientRunes)
}

// capRunes truncates s to at most n runes.
func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// What ArchiveFileGate found at the end of a path.
const (
	GateMissing   = "missing"   // the file, or a directory on the way to it, does not exist
	GateSymlink   = "symlink"   // a link at some component: never followed
	GateIrregular = "irregular" // the final component exists and is not a regular file
	GateOK        = "ok"        // a regular file, reached with no link on the way
)

// ArchiveFileGate resolves segs component by component under root and refuses to follow
// a symlink at any level (extract, verify and the repair's old-file reader all read an
// archived file through it, so none can disagree about which files exist). It returns
// what it found (a Gate* constant), the file's size for GateOK, and the path it reached.
// The check is by Lstat: a co-writer can still swap a file between this check and the
// open that follows, which the callers bound by reading no more than the recorded size.
func ArchiveFileGate(root string, segs []string) (kind string, size int64, full string) {
	cur := root
	for i, s := range segs {
		cur = filepath.Join(cur, s)
		fi, err := os.Lstat(cur)
		if err != nil {
			return GateMissing, 0, cur
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return GateSymlink, 0, cur
		}
		if i == len(segs)-1 {
			if !fi.Mode().IsRegular() {
				return GateIrregular, 0, cur
			}
			return GateOK, fi.Size(), cur
		}
		if !fi.IsDir() {
			// An intermediate component is a file: the recorded path cannot exist.
			return GateMissing, 0, cur
		}
	}
	return GateMissing, 0, cur
}
