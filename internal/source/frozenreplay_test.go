package source

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode"

	sharedmessage "github.com/emersion/go-message"
	sharedcharset "github.com/emersion/go-message/charset"

	v1charset "mail-archive-tool/internal/source/parsev1/_dep/charset"
	v1message "mail-archive-tool/internal/source/parsev1/_dep/message"
)

// The pins around the frozen v1 replay (docs/design-mime-fidelity.md §4.2, MC26,
// MC27), slice S1:
//
//   - testdata/mime/charset-labels.txt lists every label in IANA, WHATWG, go-message's
//     quirk table and go-pst's nameMap (x/text v0.42.0's tables);
//   - testdata/mime/golden-charsets.txt records how each resolves: a digest of what the
//     charset reader makes of a fixed probe, or "error";
//   - testdata/mime/parsev1.pins records unicode.Version, the sha256 of parsev1.go and
//     of every vendored file, and the go.sum lines of the identity dependencies.
//
// The last two are baselines in the CLAUDE.md sense: regenerating one is an
// operator-accepted act under the design's §5 drift policy, never a fix.
const (
	mimeCharsetLabels  = "charset-labels.txt"
	mimeGoldenCharsets = "golden-charsets.txt"
	mimeParsev1Pins    = "parsev1.pins"
)

// identityModules are the modules whose go.sum lines the replay's identity depends on
// (P1(b)): go-message and go-textwrapper (vendored, and still what the shared parser
// builds on), go-mbox's framing, go-pst's nameMap, and the tables behind the charset
// readers.
var identityModules = map[string]bool{
	"github.com/emersion/go-message":     true,
	"github.com/emersion/go-textwrapper": true,
	"github.com/emersion/go-mbox":        true,
	"github.com/mooijtech/go-pst/v6":     true,
	"golang.org/x/text":                  true,
	"golang.org/x/net":                   true,
}

// charsetProbe is the fixed input every label's reader is run over: every byte, every
// two-byte sequence with a high first byte, and the multi-byte shapes a stateful or
// four-byte charset needs (ISO-2022-JP escapes, GB18030 four-byte, UTF-16 BOM and a
// surrogate pair).
func charsetProbe() []byte {
	var b []byte
	for i := 0; i < 256; i++ {
		b = append(b, byte(i))
	}
	for hi := 0x80; hi < 0x100; hi++ {
		for lo := 0; lo < 0x100; lo++ {
			b = append(b, byte(hi), byte(lo))
		}
	}
	b = append(b, "\x1b$B0!\x1b(B"...)
	b = append(b, "\x81\x30\x81\x30\x84\x31\xa4\x39"...)
	b = append(b, "\xff\xfeA\x00\x00\xd8\x00\xdc"...)
	return b
}

// charsetDescriptor is what a charset reader makes of the probe for one label.
func charsetDescriptor(read func(string, io.Reader) (io.Reader, error), probe []byte, label string) string {
	r, err := read(label, bytes.NewReader(probe))
	if err != nil || r == nil {
		return "error"
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return "read-error"
	}
	return fmt.Sprintf("len=%d sha256=%x", len(out), sha256.Sum256(out))
}

func charsetLabels(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(mimeCorpusDir, mimeCharsetLabels))
	if err != nil {
		t.Fatalf("charset labels: %v", err)
	}
	labels := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(labels) < 500 {
		t.Fatalf("%s lists %d labels; IANA, WHATWG, the quirk table and go-pst's names make more than 500", mimeCharsetLabels, len(labels))
	}
	return labels
}

// charsetTable is the snapshot text of one charset reader over every label.
func charsetTable(t *testing.T, read func(string, io.Reader) (io.Reader, error)) string {
	t.Helper()
	probe := charsetProbe()
	var sb strings.Builder
	for _, l := range charsetLabels(t) {
		fmt.Fprintf(&sb, "%s\t%s\n", l, charsetDescriptor(read, probe, l))
	}
	return sb.String()
}

// v1FilesToPin lists parsev1.go and every file under parsev1/, relative to the
// repository, in a fixed order.
func v1FilesToPin(t *testing.T) []string {
	t.Helper()
	files := []string{"internal/source/parsev1.go"}
	err := filepath.WalkDir("parsev1", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, "internal/source/"+filepath.ToSlash(p))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk parsev1: %v", err)
	}
	return files
}

// parsev1Pins is the body the pins file must hold for the tree as it is.
func parsev1Pins(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	fmt.Fprintf(&sb, "unicode.Version %s\n", unicode.Version)
	for _, f := range v1FilesToPin(t) {
		data, err := os.ReadFile(filepath.Join("../..", filepath.FromSlash(f)))
		if err != nil {
			t.Fatalf("pin %s: %v", f, err)
		}
		fmt.Fprintf(&sb, "sha256 %x %s\n", sha256.Sum256(data), f)
	}
	sum, err := os.ReadFile("../../go.sum")
	if err != nil {
		t.Fatalf("go.sum: %v", err)
	}
	for _, l := range strings.Split(string(sum), "\n") {
		if f := strings.Fields(l); len(f) > 0 && identityModules[f[0]] {
			fmt.Fprintf(&sb, "go.sum %s\n", strings.Join(f, " "))
		}
	}
	return sb.String()
}

// requireProvenance fails unless the golden's header carries each key with a value.
func requireProvenance(t *testing.T, file string, header []string, keys ...string) {
	t.Helper()
	for _, key := range keys {
		found := false
		for _, h := range header {
			if strings.HasPrefix(h, key) && strings.TrimSpace(strings.TrimPrefix(h, key)) != "" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: the provenance header has no %q line", file, key)
		}
	}
}

// covers: R27
// The recorder for the charset snapshot and the pins. It writes only when given a
// directory, like the identity and content goldens' recorder: regenerating either is
// an operator-accepted act (design §5), never part of a test run.
func TestRecordParseV1Snapshots(t *testing.T) {
	if *mimeRecordDir == "" {
		t.Skip("records the snapshot and the pins only when -mime-record-dir is given (an operator-accepted act, design §5)")
	}
	if *mimeRecordProvenance == "" {
		t.Fatal("-mime-record-provenance is required: a baseline without provenance is not recorded")
	}
	prov, err := os.ReadFile(*mimeRecordProvenance)
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	var header strings.Builder
	for _, l := range strings.Split(strings.TrimSuffix(string(prov), "\n"), "\n") {
		header.WriteString("# " + l + "\n")
	}
	for file, body := range map[string]string{
		mimeGoldenCharsets: charsetTable(t, sharedcharset.Reader),
		mimeParsev1Pins:    parsev1Pins(t),
	} {
		if err := os.WriteFile(filepath.Join(*mimeRecordDir, file), []byte(header.String()+body), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
}

// goTool finds the go command the module is built with.
func goTool(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	p := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("no go command to list the replay's imports with (PATH, or %s): %v", p, err)
	}
	return p
}

// covers: MA-331, R27, R3
// The replay's charset table, source files and identity dependencies are pinned: the
// label table matches its snapshot for the vendored and the shared table and survives
// parsing the corpus; the sha256 of parsev1.go and every vendored file and the go.sum
// lines of the identity modules are the pinned ones; the replay imports none of
// net/mail, mime, encoding/base64 or go-message; the v1 message of every corpus item
// is the content golden; and unicode.Version is the pinned one.
func TestFrozenReplayPinsTheCharsetTableAndItsSources(t *testing.T) {
	items := loadMimeCorpus(t)

	t.Run("charset table", func(t *testing.T) {
		want, header, err := goldenBody(mimeGoldenCharsets)
		if err != nil {
			t.Fatalf("%s: %v", mimeGoldenCharsets, err)
		}
		requireProvenance(t, mimeGoldenCharsets, header, "recorded:", "toolchain:", "dependencies:")
		sharedFn := reflect.ValueOf(sharedmessage.CharsetReader).Pointer()
		v1Fn := reflect.ValueOf(v1message.CharsetReader).Pointer()
		check := func(when string) {
			t.Helper()
			if got := charsetTable(t, sharedcharset.Reader); got != want {
				t.Errorf("%s: the shared charset table no longer matches the snapshot at %s", when, firstDiff(want, got))
			}
			if got := charsetTable(t, v1charset.Reader); got != want {
				t.Errorf("%s: the vendored charset table no longer matches the snapshot at %s", when, firstDiff(want, got))
			}
			if reflect.ValueOf(sharedmessage.CharsetReader).Pointer() != sharedFn || sharedFn != reflect.ValueOf(sharedcharset.Reader).Pointer() {
				t.Errorf("%s: go-message's message.CharsetReader is no longer charset.Reader", when)
			}
			if reflect.ValueOf(v1message.CharsetReader).Pointer() != v1Fn || v1Fn != reflect.ValueOf(v1charset.Reader).Pointer() {
				t.Errorf("%s: the vendored message.CharsetReader is no longer the vendored charset.Reader", when)
			}
		}
		check("before the corpus")
		for _, it := range items {
			safeParseMessage(it.data)
			replayV1(it.data)
		}
		check("after parsing the corpus")

		// The positive twin: the harness tells a known label from an unknown one, and
		// go-pst's names are in the table the vendored copy holds.
		probe := charsetProbe()
		if d := charsetDescriptor(v1charset.Reader, probe, "windows-1252"); d == "error" {
			t.Error("the vendored table does not know windows-1252")
		}
		if d := charsetDescriptor(v1charset.Reader, probe, "unknown-8bit"); d != "error" {
			t.Errorf("the vendored table resolves unknown-8bit to %s; v1 sends a message with that label to the stub of an unknown charset", d)
		}
		if d := charsetDescriptor(v1charset.Reader, probe, "cp437"); d == "error" {
			t.Error("the vendored table lacks go-pst's cp437")
		}
	})

	t.Run("sources and dependencies", func(t *testing.T) {
		want, header, err := goldenBody(mimeParsev1Pins)
		if err != nil {
			t.Fatalf("%s: %v", mimeParsev1Pins, err)
		}
		requireProvenance(t, mimeParsev1Pins, header, "recorded:", "toolchain:", "dependencies:")
		if got := parsev1Pins(t); got != want {
			t.Errorf("the frozen replay's sources or identity dependencies no longer match %s at %s\n(an edit to parsev1.go, a vendored file or an identity dependency needs an operator decision under the design's §5 drift policy)", mimeParsev1Pins, firstDiff(want, got))
		}
	})

	t.Run("unicode tables", func(t *testing.T) {
		// The unicode tables are the toolchain's: a Go release that moves them can move
		// what the replay's string functions and x/text compute (ruling OR4).
		want := ""
		body, _, err := goldenBody(mimeParsev1Pins)
		if err != nil {
			t.Fatalf("%s: %v", mimeParsev1Pins, err)
		}
		for _, l := range strings.Split(body, "\n") {
			if v, ok := strings.CutPrefix(l, "unicode.Version "); ok {
				want = v
			}
		}
		if want == "" {
			t.Fatalf("%s pins no unicode.Version", mimeParsev1Pins)
		}
		if unicode.Version != want {
			t.Errorf("unicode.Version = %s, pinned %s: this toolchain's unicode tables are not the ones the goldens were recorded with (design §5 drift policy)", unicode.Version, want)
		}
	})

	t.Run("imports", func(t *testing.T) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "parsev1.go", nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse parsev1.go: %v", err)
		}
		var direct, vendored []string
		for _, im := range f.Imports {
			p, err := strconv.Unquote(im.Path.Value)
			if err != nil {
				t.Fatalf("import %s: %v", im.Path.Value, err)
			}
			direct = append(direct, p)
			if strings.HasPrefix(p, "mail-archive-tool/internal/source/parsev1/") {
				vendored = append(vendored, p)
			}
		}
		if len(vendored) == 0 {
			t.Fatal("parsev1.go imports none of the vendored packages")
		}
		cmd := exec.Command(goTool(t), append([]string{"list", "-deps", "-f", "{{.ImportPath}}"}, vendored...)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("go list -deps %v: %v\n%s", vendored, err, stderr.String())
		}
		have := map[string]bool{}
		for _, p := range append(direct, strings.Fields(stdout.String())...) {
			have[p] = true
		}
		for p := range have {
			if p == "net/mail" || p == "mime" || p == "encoding/base64" || p == "github.com/emersion/go-message" || strings.HasPrefix(p, "github.com/emersion/go-message/") {
				t.Errorf("the replay's import closure holds %s, which the vendored copy exists to keep out (MC27, ruling OR4)", p)
			}
		}
		// The positive twin: the closure is the vendored tree's own.
		for _, p := range []string{"message", "mail", "netmail", "mime", "base64", "charset"} {
			if !have["mail-archive-tool/internal/source/parsev1/_dep/"+p] {
				t.Errorf("the replay's import closure lacks the vendored %s", p)
			}
		}
	})

	t.Run("the v1 message of every corpus item is the content golden", func(t *testing.T) {
		want, _, err := goldenBody(mimeGoldenContent)
		if err != nil {
			t.Fatalf("%s: %v", mimeGoldenContent, err)
		}
		_, got := renderGoldens(t, items, corpusV1Parse, unreadableStub())
		// The golden holds the corpus, the stub and support.pst, which the replay does
		// not parse: compare the corpus's part, up to the stub.
		cut := func(s string) string {
			i := strings.Index(s, "== "+mimeV1StubName)
			if i < 0 {
				t.Fatal("the content golden has no stub item")
			}
			return s[:i]
		}
		if cut(got) != cut(want) {
			t.Errorf("the vendored replay no longer reproduces the content golden at %s", firstDiff(cut(want), cut(got)))
		}
	})
}
