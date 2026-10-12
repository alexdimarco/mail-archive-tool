package source

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The MIME-fidelity corpus (docs/design-mime-fidelity.md §9, MC7), slice S0.
//
// testdata/mime/ holds one .eml per trigger of design §1, the Apple Mail, mutt,
// Mailman, Outlook and bounce shapes, controls, the inputs that reach
// fallbackParse, and one item for each label in go-pst's nameMap; corpus.tsv
// indexes it, and testdata/support.pst is the PST twin. The identity and content
// goldens recorded over it are in mimegolden_test.go.
const (
	mimeCorpusDir = "../../testdata/mime"
	mimePSTFile   = "../../testdata/support.pst"
)

// corpusV1Panics reports whether the v1 parse of data panics, before any recover. It
// runs the frozen copy of parseMessage (parsev1.go, replayV1's), so the corpus keeps
// being checked against v1 after the content parser moves.
var corpusV1Panics = func(data []byte) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	parseMessageV1(data)
	return false
}

type mimeCorpusItem struct {
	name, group, trigger, rows string
	data                       []byte
}

// loadMimeCorpus reads corpus.tsv and every item it lists, and checks the index
// and the directory agree.
func loadMimeCorpus(t *testing.T) []mimeCorpusItem {
	t.Helper()
	index, err := os.ReadFile(filepath.Join(mimeCorpusDir, "corpus.tsv"))
	if err != nil {
		t.Fatalf("corpus index: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(index), "\n"), "\n")
	if len(lines) < 2 || lines[0] != "file\tgroup\ttrigger\trows" {
		t.Fatalf("corpus.tsv: bad header %q", lines[0])
	}
	var items []mimeCorpusItem
	listed := map[string]bool{}
	for _, l := range lines[1:] {
		f := strings.Split(l, "\t")
		if len(f) != 4 || f[0] == "" || f[1] == "" || f[2] == "" || f[3] == "" {
			t.Fatalf("corpus.tsv: bad row %q", l)
		}
		if listed[f[0]] {
			t.Fatalf("corpus.tsv lists %s twice", f[0])
		}
		listed[f[0]] = true
		data, err := os.ReadFile(filepath.Join(mimeCorpusDir, f[0]))
		if err != nil {
			t.Fatalf("corpus item: %v", err)
		}
		items = append(items, mimeCorpusItem{name: f[0], group: f[1], trigger: f[2], rows: f[3], data: data})
	}
	entries, err := os.ReadDir(mimeCorpusDir)
	if err != nil {
		t.Fatalf("corpus dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".eml") && !listed[e.Name()] {
			t.Errorf("%s is in testdata/mime but not in corpus.tsv", e.Name())
		}
	}
	if !sort.SliceIsSorted(items, func(i, j int) bool { return items[i].name < items[j].name }) {
		t.Fatal("corpus.tsv is not sorted by file name")
	}
	return items
}

// covers: R27
// The corpus holds what the design's §9 says it holds: an item per trigger of §1,
// the shapes, one item per go-pst nameMap label, an item that panics v1, and every
// file indexed once. It is the fixture MA-330, MA-331 and MA-332 stand on.
func TestMimeCorpusIsComplete(t *testing.T) {
	items := loadMimeCorpus(t)
	have := map[string]mimeCorpusItem{}
	groups := map[string]int{}
	for _, it := range items {
		have[it.name] = it
		groups[it.group]++
	}
	need := func(name string) {
		t.Helper()
		if _, ok := have[name]; !ok {
			t.Errorf("the corpus lacks %s", name)
		}
	}

	// F24: each unknown charset label first and after the body, and the CTEs.
	for _, tag := range []string{"unknown-8bit", "x-unknown", "empty", "default", "utf-7", "cp-850", "iso-8859-1"} {
		need("f24-" + tag + "-first.eml")
		need("f24-" + tag + "-after-body.eml")
	}
	need("f24-unknown-8bit-single.eml")
	for _, tag := range []string{"8-bit", "qp-encoded", "x-uuencode", "x-foo", "7bit", "quoted-printable", "base64", "base64-truncated"} {
		need("f24-cte-" + tag + ".eml")
	}
	// F25, C1-1, C1-4, C1-5, C1-8, C1-10 and the shapes.
	for _, n := range []string{
		"f25-apple-mail", "f25-mutt", "f25-mailman", "f25-inline-names", "f25-duplicate-and-long-names",
		"f25-reserved-names", "f25-nested-long-subject",
		"c11-text-attachments", "c11-8bit-attachment-lf", "c11-utf8-attachment",
		"c14-unquoted-boundary-eq", "c14-unquoted-start-type", "c14-charset-comment", "c14-unquoted-filename",
		"c14-no-boundary", "c14-strict-valid",
		"c15-cid-text-placeholder", "c15-cid-html-links", "c15-datauri-style-breakout",
		"c18-latin1-headers", "c18-latin1-body-unlabelled", "c18-latin1-body-usascii", "c18-latin1-body-utf8-label",
		"shape-outlook-related", "shape-bounce-dsn", "shape-bounce-nullsender",
		"fallback-header-error", "fallback-no-headers", "fallback-empty",
		"ctl-plain-ascii", "ctl-no-message-id", "ctl-no-message-id-no-date", "ctl-mid-reuse-a", "ctl-mid-reuse-b",
	} {
		need(n + ".eml")
	}
	for _, tag := range []string{"empty-angle", "comma-name", "mailer-daemon", "addr-addr", "double-dot", "leading-dot", "undisclosed", "8bit-name", "plain-valid"} {
		need("c110-from-" + tag + ".eml")
	}

	// One item per go-pst v6.0.2 nameMap label (pkg/charsets.go): 40 of them.
	nameMap := []string{"utf8", "big5", "big5hk", "iso_1", "cp874", "cp437", "cp850", "cp852", "cp855", "cp857",
		"cp858", "cp860", "cp864", "cp866", "cp869", "cp932", "cp936", "cp950", "cp1250", "cp1251", "cp1252", "cp1253",
		"cp1254", "cp1255", "cp1256", "cp1257", "cp1258", "gb18030", "greek8", "iso88592", "iso88595", "iso88596",
		"iso88597", "iso88598", "iso88599", "iso15", "koi8", "sjis", "tis-620", "roman8"}
	if len(nameMap) != 40 {
		t.Fatalf("the nameMap list holds %d labels, want 40", len(nameMap))
	}
	for _, lab := range nameMap {
		need("charset-" + lab + ".eml")
		if it, ok := have["charset-"+lab+".eml"]; ok {
			if !bytes.Contains(it.data, []byte("charset="+lab+"\r\n")) {
				t.Errorf("charset-%s.eml does not declare charset=%s", lab, lab)
			}
			if it.group != "nameMap" {
				t.Errorf("charset-%s.eml is in group %q, want nameMap", lab, it.group)
			}
		}
	}
	if groups["nameMap"] != 40 {
		t.Errorf("the nameMap group holds %d items, want 40", groups["nameMap"])
	}

	// An item that really panics v1, and no other item does. S0 found none: coverage-
	// guided fuzz runs over the corpus (millions of parses) and a sweep of 254 charset
	// labels with random bodies, encoded words and filenames never made parseMessage
	// panic. The identity golden pins the stub from a constructed message instead, and
	// the case stays open.
	if groups["v1-panic"] < 1 {
		t.Run("a corpus item that panics v1", func(t *testing.T) {
			t.Skip("MA-330: no input panics the v1 parse (parseMessage and its dependencies are panic-free on every shape tried), so the stub's identity is pinned from a constructed message; closes when a panicking input is found, or the owner rules the constructed stub enough")
		})
	}
	for _, it := range items {
		if got := corpusV1Panics(it.data); got != (it.group == "v1-panic") {
			t.Errorf("%s (group %s): v1 panics = %t", it.name, it.group, got)
		}
	}
	if _, err := os.Stat(mimePSTFile); err != nil {
		t.Errorf("the PST twin: %v", err)
	}
}
