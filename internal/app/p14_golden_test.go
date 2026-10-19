package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/state"
)

// p14GoldenFile is the pre-change golden of MA-329 (fault-isolation P14): what a
// healthy input produced BEFORE fault isolation's code existed — taken from the
// tree at fix/review-2026-10 (46e7de6), with no S1a code in it. Rewriting it is
// the operator's act, never a way to make a red gate green (CLAUDE.md): the flag
// below exists only to take a golden from a tree that holds no change to the
// healthy path.
const p14GoldenFile = "testdata/p14-golden.json"

var writeP14Golden = flag.Bool("p14-golden-write", false, "MA-329: write the pre-change golden instead of comparing with it")

// p14Fixture is what one healthy input leaves in an archive that P14 says must
// not change: every manifest record (keys, identities, paths, completeness
// fields, fingerprints, fixity) with its timestamps masked, and the SHA-256 of
// every per-message file (page, attachment zip, preserved original) and of the
// verification report when there is one. The derived views P14 names as its
// exceptions — README.txt and every index.html — are not part of it.
type p14Fixture struct {
	Entries map[string]state.Record `json:"entries"`
	Files   map[string]string       `json:"files"`
}

// p14Mail builds one RFC 5322 message; id "" leaves the Message-ID out (the
// content-hash identity).
func p14Mail(subject, id, extraHeaders, body string) string {
	h := "From: Alice <alice@example.com>\r\nTo: Bob <bob@example.com>\r\nSubject: " + subject +
		"\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n"
	if id != "" {
		h += "Message-ID: <" + id + "@ex>\r\n"
	}
	return h + extraHeaders + "\r\n" + body + "\r\n"
}

// p14Plain, p14HTML and p14Attached are the three shapes the fixtures hold: a
// plain message, an HTML one, and one with an attachment (a zip is written).
func p14Plain(subject, id string) string {
	return p14Mail(subject, id, "Content-Type: text/plain; charset=utf-8\r\n", "plain body of "+subject)
}

func p14HTML(subject, id string) string {
	return p14Mail(subject, id, "Content-Type: text/html; charset=utf-8\r\n", "<p>Hello <b>"+subject+"</b></p>")
}

func p14Attached(subject, id string) string {
	return p14Mail(subject, id, "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"B1\"\r\n",
		"--B1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nSee the attachment.\r\n"+
			"--B1\r\nContent-Type: text/plain; name=\"report.txt\"\r\nContent-Disposition: attachment; filename=\"report.txt\"\r\n"+
			"Content-Transfer-Encoding: base64\r\n\r\nSGVsbG8gYXR0YWNobWVudA==\r\n--B1--")
}

func p14Write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// p14Mbox is a Thunderbird-style store: two mbox files and a nested .sbd folder.
// It holds a message filed in two folders (R3), a message with no Message-ID, one
// with an attachment, one HTML message, and two distinct messages that reuse one
// Message-ID in one folder (S23, the #fp-qualified key).
func p14Mbox(t *testing.T, parent string) string {
	dir := filepath.Join(parent, "tbstore")
	from := "From alice@example.com Mon Mar  3 09:00:00 2025\n"
	mb := func(msgs ...string) string {
		var b strings.Builder
		for _, m := range msgs {
			b.WriteString(from)
			b.WriteString(strings.ReplaceAll(m, "\r\n", "\n"))
			b.WriteString("\n")
		}
		return b.String()
	}
	p14Write(t, filepath.Join(dir, "Inbox"), mb(
		p14Plain("First", "first"), p14HTML("Second", "second"), p14Attached("Third", "third"),
		p14Plain("Nameless", ""), p14Plain("Reuse A", "reuse"), p14Plain("Reuse B", "reuse")))
	p14Write(t, filepath.Join(dir, "Archive"), mb(p14Plain("Archived", "archived")))
	p14Write(t, filepath.Join(dir, "Archive.sbd", "Old"), mb(p14Plain("First", "first")))
	return dir
}

// p14Maildir is one maildir folder, read from cur/ and new/.
func p14Maildir(t *testing.T, parent string) string {
	dir := filepath.Join(parent, "maildirstore")
	p14Write(t, filepath.Join(dir, "cur", "m1"), p14Plain("Maildir one", "md1"))
	p14Write(t, filepath.Join(dir, "cur", "m2"), p14Attached("Maildir two", "md2"))
	p14Write(t, filepath.Join(dir, "new", "m3"), p14HTML("Maildir three", "md3"))
	return dir
}

// p14EvolutionMaildirPP is an Evolution local store (Maildir++), with a nested
// dot-encoded folder.
func p14EvolutionMaildirPP(t *testing.T, parent string) string {
	dir := filepath.Join(parent, "local")
	p14Write(t, filepath.Join(dir, "maildirfolder"), "")
	p14Write(t, filepath.Join(dir, "cur", "r1"), p14Plain("Root inbox", "evo-root"))
	p14Write(t, filepath.Join(dir, ".Work", "cur", "w1"), p14Plain("Work item", "evo-work"))
	p14Write(t, filepath.Join(dir, ".Work.Projects", "cur", "p1"), p14Attached("Project item", "evo-proj"))
	return dir
}

// p14EvolutionCache is an Evolution IMAP disk cache with a sharded cur/ and a
// "subfolders" container.
func p14EvolutionCache(t *testing.T, parent string) string {
	dir := filepath.Join(parent, "cacheacct")
	folders := filepath.Join(dir, "folders")
	p14Write(t, filepath.Join(folders, "INBOX", "cur", "16", "3437"), p14Plain("Cache sharded", "evo-shard"))
	p14Write(t, filepath.Join(folders, "INBOX", "new", "n1"), p14HTML("Cache new", "evo-new"))
	p14Write(t, filepath.Join(folders, "Gmail", "subfolders", "All", "cur", "g1"), p14Plain("Cache nested", "evo-nested"))
	return dir
}

// p14Collect reads the fixture an archive holds: records with their timestamps
// masked, and the hash of every per-message file and of the report.
func p14Collect(t *testing.T, out string) p14Fixture {
	t.Helper()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	// A healthy input leaves no gap behind: the ledger is empty (MA-329).
	if n := m.GapCount(); n != 0 {
		t.Errorf("a healthy input left %d capture gap(s) in the ledger: %+v", n, m.CaptureGapsSorted())
	}
	f := p14Fixture{Entries: map[string]state.Record{}, Files: map[string]string{}}
	for k, r := range m.All() {
		r.ExportedAt, r.FirstSeen, r.LastSeen = time.Time{}, time.Time{}, time.Time{}
		f.Entries[k] = r
	}
	err = filepath.WalkDir(out, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		name := d.Name()
		perMessage := (strings.HasSuffix(name, ".html") && !strings.HasPrefix(name, "index")) ||
			strings.HasSuffix(name, ".eml") || strings.HasSuffix(name, "-attachments.zip")
		if !perMessage && name != "attachments-report.tsv" {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(out, path)
		sum := sha256.Sum256(b)
		f.Files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// p14Local archives one local input twice. The first run fills the archive; the
// second, incremental, must export nothing (R2).
func p14Local(t *testing.T, input string) p14Fixture {
	t.Helper()
	out := tmpDir(t)
	opts := Options{Inputs: []string{input}, Out: out, Mode: export.Incremental, KeepRaw: true}
	first, err := Run(context.Background(), opts, discard(), nil)
	if err != nil {
		t.Fatalf("first run over %s: %v", input, err)
	}
	if first.Stats.Exported == 0 {
		t.Fatalf("the first run over %s exported nothing: the fixture is not read", input)
	}
	second, err := Run(context.Background(), opts, discard(), nil)
	if err != nil {
		t.Fatalf("second run over %s: %v", input, err)
	}
	if second.Stats.Exported != 0 {
		t.Errorf("an incremental re-run over the unchanged %s exported %d, want 0 (R2)", input, second.Stats.Exported)
	}
	return p14Collect(t, out)
}

// p14Graph archives the fake Graph server's two mailboxes twice; one message is
// served with an attachment.
func p14Graph(t *testing.T) p14Fixture {
	t.Helper()
	s := graphtest.NewServer()
	t.Cleanup(s.Close)
	s.Fixture()
	s.SetValue("U1IN1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, p14Attached("subj-U1IN1", "u1in1"))
	})
	g := GraphOptions{
		Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1", "u2"},
		BaseURL: s.URL, TokenURL: s.URL + "/token",
	}
	out := tmpDir(t)
	opts := Options{Out: out, Mode: export.Incremental, KeepRaw: true}
	first, err := RunGraph(context.Background(), g, opts, discard())
	if err != nil || first.Stats.Exported != 12 {
		t.Fatalf("the first Graph run: exported %d, err %v; want 12, nil", first.Stats.Exported, err)
	}
	second, err := RunGraph(context.Background(), g, opts, discard())
	if err != nil {
		t.Fatalf("the second Graph run: %v", err)
	}
	if second.Stats.Exported != 0 {
		t.Errorf("an incremental Graph re-run exported %d, want 0 (R2)", second.Stats.Exported)
	}
	return p14Collect(t, out)
}

// covers: MA-329, R2, R3, R5
// P14: a healthy input is unchanged by fault isolation. On support.pst and the
// mbox, maildir, Evolution (Maildir++ and cache) and fake-Graph fixtures, every
// record (key, identity, path, completeness fields, fingerprint, fixity) and the
// bytes of every per-message file equal the golden taken before any S1a code
// (timestamps masked; README.txt and the root index.html stamp are P14's named
// exceptions and not compared); an incremental re-run exports 0 and the ledger
// stays empty.
func TestHealthyInputsMatchTheGolden(t *testing.T) {
	parent := tmpDir(t)
	got := map[string]p14Fixture{
		"pst":            p14Local(t, "../../testdata/support.pst"),
		"mbox":           p14Local(t, p14Mbox(t, parent)),
		"maildir":        p14Local(t, p14Maildir(t, parent)),
		"evolution-pp":   p14Local(t, p14EvolutionMaildirPP(t, parent)),
		"evolution-imap": p14Local(t, p14EvolutionCache(t, parent)),
		"graph":          p14Graph(t),
	}
	for name, f := range got {
		if len(f.Entries) == 0 || len(f.Files) == 0 {
			t.Fatalf("fixture %s archived nothing (%d records, %d files): the golden would prove nothing", name, len(f.Entries), len(f.Files))
		}
	}
	gotJSON, err := json.MarshalIndent(got, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	gotJSON = append(gotJSON, '\n')

	if *writeP14Golden {
		if err := os.WriteFile(p14GoldenFile, gotJSON, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d bytes)", p14GoldenFile, len(gotJSON))
		return
	}
	want, err := os.ReadFile(p14GoldenFile)
	if err != nil {
		t.Fatalf("the pre-change golden %s is missing: %v", p14GoldenFile, err)
	}
	if string(want) == string(gotJSON) {
		return
	}
	var wantFx map[string]p14Fixture
	if err := json.Unmarshal(want, &wantFx); err != nil {
		t.Fatalf("the golden %s is unreadable: %v", p14GoldenFile, err)
	}
	for _, name := range p14SortedKeys(got) {
		w, ok := wantFx[name]
		if !ok {
			t.Errorf("fixture %s is not in the golden", name)
			continue
		}
		for k, r := range got[name].Entries {
			wr, ok := w.Entries[k]
			if !ok {
				t.Errorf("%s: record %q is new (a healthy input must produce the same records)", name, k)
				continue
			}
			a, _ := json.Marshal(r)
			b, _ := json.Marshal(wr)
			if string(a) != string(b) {
				t.Errorf("%s: record %q differs\n got  %s\n want %s", name, k, a, b)
			}
		}
		for k := range w.Entries {
			if _, ok := got[name].Entries[k]; !ok {
				t.Errorf("%s: record %q is gone", name, k)
			}
		}
		for p, h := range got[name].Files {
			if wh, ok := w.Files[p]; !ok {
				t.Errorf("%s: file %s is new", name, p)
			} else if wh != h {
				t.Errorf("%s: file %s differs from the golden (sha256 %s, want %s)", name, p, h, wh)
			}
		}
		for p := range w.Files {
			if _, ok := got[name].Files[p]; !ok {
				t.Errorf("%s: file %s is gone", name, p)
			}
		}
	}
	for name := range wantFx {
		if _, ok := got[name]; !ok {
			t.Errorf("golden fixture %s was not produced", name)
		}
	}
	if !t.Failed() {
		t.Errorf("the output differs from %s but no record or file differs: the golden's encoding changed", p14GoldenFile)
	}
}

func p14SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
