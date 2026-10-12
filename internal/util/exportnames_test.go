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

// covers: MA-417, R4, R29
// E6-AO8, E6-AO9: an attachment name is chosen by whoever sent the mail, and it is written to disk, to
// SHA256SUMS.txt and to the manifest, and echoed by `sha256sum -c`, `ls` and `find` into a terminal.
// The export-only pass strips, as well as the bidi embeddings and isolates, what a terminal or a
// line splitter acts on: DEL and the C1 controls (U+009B is CSI to a terminal in UTF-8 mode, U+0085
// is NEL), the line and paragraph separators (str.splitlines breaks SHA256SUMS.txt at them), and the
// invisible marks (the bidi marks LRM, RLM and ALM that make a name read backwards in a list, the
// zero-width space, the byte order mark); and a name that starts with "-" is prefixed, so a glob in
// the attachments folder (`tar czf x.tgz *`) never takes an attachment for an option. Ordinary names,
// and the zero-width non-joiner and joiner that Persian, Indic and emoji text need, pass unchanged.
func TestExportFilenameStripsControlsAndNeverStartsWithADash(t *testing.T) {
	c := func(r rune) string { return string(r) }
	for _, ok := range []string{"report.pdf", "über-Änderung.docx", "日本語.pdf", "a-b.txt", "x.tar.gz", "mid-dash.txt", "$(id).txt",
		"persian" + c(0x200c) + "name.txt", "emoji" + c(0x200d) + "joined.png"} {
		if got := ExportFilename(ok, 1); got != ok {
			t.Errorf("ExportFilename(%q) = %q, an ordinary name must pass unchanged", ok, got)
		}
	}
	cases := []struct{ in, want string }{
		{"x" + c(0x9b) + "2Jy.txt", "x2Jy.txt"},
		{"line" + c(0x85) + "two.txt", "linetwo.txt"},
		{"sep" + c(0x2028) + "two.txt", "septwo.txt"},
		{"para" + c(0x2029) + "two.txt", "paratwo.txt"},
		{"del\x7fete.txt", "delete.txt"},
		{"invoice" + c(0x200f) + "fdp.exe", "invoicefdp.exe"},
		{"invoice" + c(0x200e) + "fdp.exe", "invoicefdp.exe"},
		{"invoice" + c(0x61c) + "fdp.exe", "invoicefdp.exe"},
		{c(0xfeff) + "bom.txt", "bom.txt"},
		{"zero" + c(0x200b) + "width.txt", "zerowidth.txt"},
		{"--checkpoint=1", "_--checkpoint=1"},
		{"--checkpoint-action=exec=sh shell.sh", "_--checkpoint-action=exec=sh shell.sh"},
		{"-rf", "_-rf"},
		{c(0x200f) + "-rf", "_-rf"},
		{"-", "_-"},
		{strings.Repeat("-", 300) + ".pdf", ""},
		{c(0x9b) + strings.Repeat("x", 300) + ".pdf", ""},
	}
	for _, tc := range cases {
		got := ExportFilename(tc.in, 1)
		exportNameOK(t, tc.in, got)
		for _, r := range got {
			if r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x2028 || r == 0x2029 || r == 0x200b || r == 0x200e || r == 0x200f || r == 0x061c || r == 0xfeff {
				t.Errorf("%q → %q keeps U+%04X", tc.in, got, r)
			}
		}
		if strings.HasPrefix(got, "-") {
			t.Errorf("%q → %q starts with a dash", tc.in, got)
		}
		if tc.want != "" && got != tc.want {
			t.Errorf("ExportFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
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
