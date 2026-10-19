package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-420, R20, R29, R4, S34, S52
// Archive originals (design §4.6, §9): a -raw record's .eml is copied byte-exact after the gate; a
// record the archive kept no original of exports its archived page and its zip byte-exact, status
// exported-without-original, with the reason; nothing is synthesized. The positive twin runs first:
// the same archive, whole, exports clean and its manifest, sums and files agree.
func TestArchiveSourceOriginalsAndTheArchivedPageGate(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	s := a.open()
	keys := allKeys(s)
	if len(keys) != 6 {
		t.Fatalf("the fixture archive holds %d records, want 6", len(keys))
	}
	p := a.plan(e, s, keys)
	if c := p.Archive; c == nil || c.WithOriginal != 3 || c.WithoutOriginal != 3 || c.Quarantined != 0 || c.Gone != 0 {
		t.Fatalf("the preview's archive counts: %+v, want 3 with an original and 3 without, none quarantined", c)
	}
	res := a.run(e, p, s)
	if res.Status != CopyStatusComplete || res.Counts.Exported != 6 || res.Counts.WithoutOriginal != 3 {
		t.Fatalf("the run: %+v", res)
	}
	man := manifestOf(t, res.Root)
	if src := man["source"].(map[string]any); src["kind"] != "archive" || src["archive"] != a.dir || len(src["stores"].([]any)) != 2 {
		t.Errorf("the manifest's source: %v", src)
	}
	for _, slug := range []string{"hello", "second", "client-mail"} {
		key, rec := a.rec(slug)
		ent := entryOf(t, man, key)
		orig := fileOf(ent, "original")
		if ent["status"] != "exported" || orig == nil || len(ent["files"].([]any)) != 1 {
			t.Errorf("%s: entry %v, want one original and status exported", slug, ent)
			continue
		}
		_, eml, _ := a.files(slug)
		got := readText(t, res.Root, orig["path"].(string))
		if got != string(a.read(eml)) || orig["sha256"] != rec.Fixity.EML.SHA256 || !strings.HasSuffix(orig["path"].(string), ".eml") {
			t.Errorf("%s: the .eml is not the archive's byte for byte (hash %v, recorded %s)", slug, orig["sha256"], rec.Fixity.EML.SHA256)
		}
	}
	att := entryOf(t, man, a.key("with-attachment"))
	page, zip := fileOf(att, "archived-page"), fileOf(att, "archived-attachments-zip")
	if att["status"] != "exported-without-original" || page == nil || zip == nil || fileOf(att, "original") != nil {
		t.Fatalf("with-attachment: %v, want its page and zip, status exported-without-original, no original", att)
	}
	_, rec := a.rec("with-attachment")
	pagePath, _, zipPath := a.files("with-attachment")
	if page["sha256"] != rec.Fixity.HTML.SHA256 || zip["sha256"] != rec.Fixity.Zip.SHA256 ||
		readText(t, res.Root, page["path"].(string)) != string(a.read(pagePath)) || readText(t, res.Root, zip["path"].(string)) != string(a.read(zipPath)) ||
		!strings.HasSuffix(page["path"].(string), ".html") || !strings.HasSuffix(zip["path"].(string), "-attachments.zip") {
		t.Errorf("the archived page and zip are not the archive's, byte for byte, with the archive's hashes: %v %v", page, zip)
	}
	if n := notesOf(att); !strings.Contains(n, "captured without 'Keep each message's original .eml'") {
		t.Errorf("the entry's notes do not give the source-specific reason: %q", n)
	}
	for f := range treeFiles(t, res.Root) {
		if strings.Contains(f, "messages/bob/") && strings.HasSuffix(f, ".eml") {
			t.Errorf("an .eml %s was made for a record that has none: nothing is synthesized", f)
		}
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("the export does not verify: changed %v missing %v extra %v", bad, missing, extra)
	}

	// each of these is a listed failure, never emitted, and the message beside it is exported
	type damage struct {
		name   string
		mutate func(a *ceArchive, e *CopyExportEnv)
		class  string
		reason string
	}
	cases := []damage{
		{"a fixity mismatch", func(a *ceArchive, e *CopyExportEnv) {
			_, eml, _ := a.files("hello")
			b := a.read(eml)
			b[len(b)-3] ^= 0x20
			os.WriteFile(eml, b, 0o644)
		}, "terminal", "doesn't match the checksum the archive recorded"},
		{"a record a capture rewrote after the plan", func(a *ceArchive, e *CopyExportEnv) {
			_, eml, _ := a.files("hello")
			b := append(a.read(eml), []byte("X-Later: a re-capture\r\n")...)
			os.WriteFile(eml, b, 0o644)
			a.setRec("hello", func(r *state.Record) { r.Fixity.EML = digestOf(b) })
		}, "retryable", "changed in the archive during export"},
		{"a symlinked .eml", func(a *ceArchive, e *CopyExportEnv) {
			_, eml, _ := a.files("hello")
			_, other, _ := a.files("second")
			os.Remove(eml)
			if err := os.Symlink(other, eml); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}, "terminal", "a link stands in the archive"},
		{"a file with more than one hard link", func(a *ceArchive, e *CopyExportEnv) {
			_, eml, _ := a.files("hello")
			if err := os.Link(eml, eml+".also"); err != nil {
				t.Skipf("hard links unavailable: %v", err)
			}
		}, "terminal", "more than one hard link"},
		{"one over the size cap", func(a *ceArchive, e *CopyExportEnv) { e.sizeCap = 100 }, "terminal", "larger than this version of MailArchive exports"},
		{"a file removed after the preview", func(a *ceArchive, e *CopyExportEnv) {
			_, eml, _ := a.files("hello")
			os.Remove(eml)
		}, "terminal", "removed from the archive after you selected it"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := buildCEArchive(t)
			e := newCEEnv(t)
			s := a.open()
			hello, second := a.key("hello"), a.key("second")
			keys := []string{hello, second}
			if c.name == "one over the size cap" {
				keys = []string{hello} // every file of this archive is over a 100-byte cap
			}
			p := a.plan(e, s, keys)
			c.mutate(a, e) // after the preview, as a capture, a redaction or bit-rot does
			res := a.run(e, p, s)
			man := manifestOf(t, res.Root)
			f := failureListed(man, hello)
			if f == nil {
				t.Fatalf("hello is not listed as failed: %v", man["failures"])
			}
			if f["class"] != c.class || !strings.Contains(f["reason"].(string), c.reason) {
				t.Errorf("hello's failure = %v, want class %s and %q", f, c.class, c.reason)
			}
			for name := range treeFiles(t, res.Root) {
				if strings.Contains(name, "_hello_") {
					t.Errorf("a file of the failed message was emitted: %s", name)
				}
			}
			if len(keys) == 2 && entryOf(t, man, second)["status"] != "exported" {
				t.Errorf("the message beside it was not exported: %v", entryOf(t, man, second))
			}
			if left := tempResidue(t, res.Root); len(left) != 0 {
				t.Errorf("a failed message left temp files: %v", left)
			}
		})
	}
}

// covers: MA-420, R20, R29, R4, S52
// The gate's component-wise Lstat walk refuses a symlink at any level, not only the file's own (design
// §4.6 item 1, XC19): a folder directory or the store's own directory replaced, after the preview, by a
// link to a copy of itself inside the archive (which os.Root follows, and whose bytes match the recorded
// fixity, so that only the walk can tell) makes each message beneath it a listed failure, never emitted,
// while the message beside it is exported.
func TestArchiveSourceRefusesALinkAtAnyLevelOfAPath(t *testing.T) {
	for _, c := range []struct {
		name   string
		bad    string // the slug whose path is replaced by a link
		beside string
		dir    func(a *ceArchive, slug string) string // the directory that becomes a link
	}{
		{"a folder directory", "client-mail", "second", func(a *ceArchive, slug string) string {
			_, eml, _ := a.files(slug)
			return filepath.Dir(eml) // the month directory, below the folder's
		}},
		{"the store's own directory", "hello", "plain-page", func(a *ceArchive, slug string) string {
			_, eml, _ := a.files(slug)
			rel, _ := filepath.Rel(a.dir, eml)
			return filepath.Join(a.dir, strings.SplitN(filepath.ToSlash(rel), "/", 2)[0])
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := buildCEArchive(t)
			e := newCEEnv(t)
			s := a.open()
			bad, beside := a.key(c.bad), a.key(c.beside)
			p := a.plan(e, s, []string{bad, beside})
			dir := c.dir(a, c.bad)
			if err := os.Rename(dir, dir+".real"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Base(dir)+".real", dir); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			res := a.run(e, p, s)
			man := manifestOf(t, res.Root)
			f := failureListed(man, bad)
			if f == nil || f["class"] != "terminal" || !strings.Contains(f["reason"].(string), "a link stands in the archive") {
				t.Fatalf("%s: the message beneath a link is %v, want a terminal failure naming the link", c.name, f)
			}
			for name := range treeFiles(t, res.Root) {
				if strings.Contains(name, "_"+c.bad+"_") {
					t.Errorf("a file beneath a link was emitted: %s", name)
				}
			}
			if entryOf(t, man, beside)["status"] == "" || failureListed(man, beside) != nil {
				t.Errorf("the message beside it was not exported: %v", failureListed(man, beside))
			}
			if left := tempResidue(t, res.Root); len(left) != 0 {
				t.Errorf("a failed message left temp files: %v", left)
			}
		})
	}
}
