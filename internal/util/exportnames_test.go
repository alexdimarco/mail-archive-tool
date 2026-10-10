package util

import (
	"strings"
	"testing"
	"unicode/utf8"

	"mail-archive-tool/internal/assure"
)

// hostileNames are MA-417's hostile folder and attachment names.
var hostileNames = []string{
	"..", "/", "\\", "/etc/passwd", "C:\\Windows\\win.ini", "a\x00b", "NUL", "CON", "COM1.txt",
	"trailing dot.", "trailing space ", strings.Repeat("日", 100), // 300 bytes of CJK
	"evil\u202Egnp.exe", "COM¹", "CONIN$", "CON .txt",
}

// exportNameOK is what every name an export writes must satisfy: one non-empty
// segment, no separator, not "." or "..", within the byte bound, valid UTF-8, no
// bidi control, no trailing dot or space and no reserved device name (F75, F91,
// F111 on top of the base pass).
func exportNameOK(t *testing.T, in, got string) {
	t.Helper()
	switch {
	case got == "" || got == "." || got == "..":
		t.Errorf("%q → %q: not a creatable name", in, got)
	case strings.ContainsAny(got, "/\\\x00"):
		t.Errorf("%q → %q: holds a separator or NUL", in, got)
	case len(got) > ExportSegmentMaxBytes:
		t.Errorf("%q → %q: %d bytes, over the %d-byte segment bound (F91)", in, got, len(got), ExportSegmentMaxBytes)
	case !utf8.ValidString(got):
		t.Errorf("%q → %q: invalid UTF-8", in, got)
	case StripBidi(got) != got:
		t.Errorf("%q → %q: keeps a bidi control (F75)", in, got)
	case strings.HasSuffix(got, ".") || strings.HasSuffix(got, " "):
		t.Errorf("%q → %q: ends in a dot or space", in, got)
	case isReservedStrict(got):
		t.Errorf("%q → %q: is a reserved device name (F111)", in, got)
	}
}

// covers: MA-417, R4, R6, R29
// The export-only naming pass (ruling Q15): every hostile name MA-417 lists comes
// out as a distinct, creatable, in-bound segment, as a folder name and as an
// attachment name, with no archive migration. The positive twin first: an
// ordinary name is untouched by the stricter pass.
func TestExportNamesHostile(t *testing.T) {
	for _, ok := range []string{"Inbox", "Clients", "Q1 report", "über-Änderung", "report.pdf"} {
		if got := ExportSegment(ok); got != ok {
			t.Errorf("ExportSegment(%q) = %q, an ordinary name must pass unchanged", ok, got)
		}
	}
	seen := map[string]string{}
	for _, in := range assure.Reached(t, hostileNames, "hostile names") {
		got := ExportSegment(in)
		exportNameOK(t, in, got)
		// A name with nothing usable in it ("..", "/", "\") is "untitled" whatever it
		// was; siblings that collide that way are told apart by the namer's fold-key
		// disambiguation (the export test), not here.
		if prev, dup := seen[got]; dup && got != "untitled" {
			t.Errorf("%q and %q both became %q: names must stay distinct", prev, in, got)
		}
		seen[got] = in
		exportNameOK(t, in, ExportFilename(in, 3))
	}
	want := map[string]string{"COM¹": "_COM¹", "CONIN$": "_CONIN$", "CON .txt": "_CON .txt", "CON": "_CON"}
	for in, w := range want {
		if got := ExportSegment(in); got != w {
			t.Errorf("ExportSegment(%q) = %q, want %q", in, got, w)
		}
	}
	// A bidi override is stripped, and the stripped name still differs from the plain one.
	if a, b := ExportSegment("evil\u202Egnp.exe"), ExportSegment("evilgnp.exe"); a == b {
		t.Errorf("a name with an RLO and the same name without both became %q", a)
	}
}

// covers: MA-417, MA-441, R4, R29
// F91: a name over 255 bytes is cut at a rune boundary, never inside a UTF-8
// sequence, keeps a hash of the original so two long names never merge, and the
// byte bound holds again after a disambiguator is joined on.
func TestExportSegmentByteBound(t *testing.T) {
	a := strings.Repeat("日", 110) + "A"
	b := strings.Repeat("日", 110) + "B"
	ga, gb := ExportSegment(a), ExportSegment(b)
	exportNameOK(t, a, ga)
	exportNameOK(t, b, gb)
	if ga == gb {
		t.Errorf("two long names became one segment %q", ga)
	}
	j := JoinSuffix(ga, "~deadbeef", ExportSegmentMaxBytes)
	if len(j) > ExportSegmentMaxBytes || !utf8.ValidString(j) || !strings.HasSuffix(j, "~deadbeef") {
		t.Errorf("JoinSuffix = %q (%d bytes): the suffix must survive within the bound", j, len(j))
	}
	long := strings.Repeat("x", 300) + ".pdf"
	if got := ExportFilename(long, 0); len(got) > ExportSegmentMaxBytes || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("ExportFilename of a 304-byte name = %q (%d bytes), want the extension kept within the bound", got, len(got))
	}
}
