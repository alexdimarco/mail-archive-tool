package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The shared primitives of design-mime-fidelity MC35: the rules live here and every
// site calls them, so a copy cannot drift.

// covers: MA-340, MA-12, R7, R8
// DecodeLegacy is the one "valid UTF-8 as is, else Windows-1252" step: the PST reader's
// decodeBytes and the raw-MIME body decoder both call it.
func TestDecodeLegacyIsUTF8ElseWindows1252(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []byte
		want string
	}{
		{"valid UTF-8 passes through", []byte("<p>héllo · 世界</p>"), "<p>héllo · 世界</p>"},
		{"Windows-1252 smart quotes", []byte{0x93, 'h', 'i', 0x94}, "“hi”"},
		{"Latin-1 umlauts", []byte("Gr\xfc\xdfe aus K\xf6ln"), "Grüße aus Köln"},
		{"the euro sign", []byte{0x80}, "€"},
		{"empty", nil, ""},
	} {
		got := DecodeLegacy(c.in)
		if got != c.want {
			t.Errorf("%s: DecodeLegacy(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s: result is not valid UTF-8: %q", c.name, got)
		}
	}
}

// covers: MA-341, R7, R8
// LenientAddress is the one extraction of a sender from a From value net/mail
// rejects: the postmark of `extract` and the page's sender both call it. It never
// fails, strips controls and caps both results at 1,000 runes.
func TestLenientAddressRules(t *testing.T) {
	for _, c := range []struct{ in, name, email string }{
		{"<>", "<>", ""},
		{"Mueller, Hans <hans@example.de>", "Mueller, Hans", "hans@example.de"},
		{"Mail Delivery System <MAILER-DAEMON>", "Mail Delivery System", "MAILER-DAEMON"},
		{"hans@example.de <hans@example.de>", "hans@example.de", "hans@example.de"},
		{"<john..smith@example.com>", "", "john..smith@example.com"},
		{"<.john@example.com>", "", ".john@example.com"},
		{"Undisclosed sender:;", "Undisclosed sender:;", ""},
		{"a@b.example", "a@b.example", "a@b.example"},
		{`"Quoted, Name" <q@x.example>`, "Quoted, Name", "q@x.example"},
		{"Ha\x00ns <h\x01@x.example>", "Hans", "h@x.example"},
		{"Name <not an address>", "Name", ""},
		{"a <b@x.example> <c@y.example>", "a <b@x.example>", "c@y.example"},
		{"", "", ""},
	} {
		name, email := LenientAddress(c.in)
		if name != c.name || email != c.email {
			t.Errorf("LenientAddress(%q) = (%q, %q), want (%q, %q)", c.in, name, email, c.name, c.email)
		}
	}

	long := strings.Repeat("é", 2000)
	name, email := LenientAddress(long + " <" + long + "@x>")
	if utf8.RuneCountInString(name) != 1000 || utf8.RuneCountInString(email) != 1000 {
		t.Errorf("caps: name %d runes, email %d runes, want 1000 each", utf8.RuneCountInString(name), utf8.RuneCountInString(email))
	}
}

// covers: R4, R13, R20
// ArchiveFileGate is the one read-side gate of archived files: a regular file, no
// symlink at any component, found under the computed directory. extract, verify and
// re-capture integrity's old-zip reader share it.
func TestArchiveFileGateRefusesLinksAndIrregularFiles(t *testing.T) {
	root := t.TempDir()
	write := func(rel, data string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("store/Inbox/a.html", "page")
	write("outside/b.html", "elsewhere")
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "store", "linked")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "outside", "b.html"), filepath.Join(root, "store", "Inbox", "link.html")); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name string
		segs []string
		kind string
		size int64
	}{
		{"a regular file", []string{"store", "Inbox", "a.html"}, GateOK, 4},
		{"absent", []string{"store", "Inbox", "nope.html"}, GateMissing, 0},
		{"a file where a directory should be", []string{"store", "Inbox", "a.html", "x"}, GateMissing, 0},
		{"a symlink at the file", []string{"store", "Inbox", "link.html"}, GateSymlink, 0},
		{"a symlink at a directory component", []string{"store", "linked", "b.html"}, GateSymlink, 0},
		{"a directory at the file", []string{"store", "Inbox"}, GateIrregular, 0},
	} {
		kind, size, full := ArchiveFileGate(root, c.segs)
		if kind != c.kind || size != c.size {
			t.Errorf("%s: kind %q size %d, want %q size %d", c.name, kind, size, c.kind, c.size)
		}
		if kind == GateOK && full != filepath.Join(root, "store", "Inbox", "a.html") {
			t.Errorf("%s: resolved path %q", c.name, full)
		}
	}
}
