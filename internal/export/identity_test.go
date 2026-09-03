package export

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

func fullExporter(out string, m *state.Manifest) *Exporter {
	e := incExporter(out, m)
	e.Mode = Full
	return e
}

func sameID(subject, body string) *model.Message {
	return &model.Message{Subject: subject, Received: testDate, InternetMessageID: "<dup@x>", PlainBody: body}
}

// covers: MA-86, R3, R1, S23
// Two different messages that share one Message-ID in one folder are both
// exported and recorded (the second under a content-qualified key); an
// incremental re-run exports zero; a full re-run in the opposite order keeps
// the same file names (keys are anchored by the record's fingerprint).
func TestMessageIDCollisionKeepsBoth(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	a, b := sameID("First", "alpha"), sameID("Second", "beta")

	e1 := incExporter(out, manifest)
	for _, m := range []*model.Message{a, b} {
		if _, err := e1.Export("store", []string{"Inbox"}, m); err != nil {
			t.Fatal(err)
		}
	}
	if e1.Stats.Exported != 2 || manifest.Len() != 2 || countSuffix(t, out, ".html") != 2 {
		t.Fatalf("collision lost a message: exported=%d manifest=%d html=%d", e1.Stats.Exported, manifest.Len(), countSuffix(t, out, ".html"))
	}
	names1 := htmlNames(t, out)

	e2 := incExporter(out, manifest)
	for _, m := range []*model.Message{a, b} {
		if _, err := e2.Export("store", []string{"Inbox"}, m); err != nil {
			t.Fatal(err)
		}
	}
	if e2.Stats.Exported != 0 || e2.Stats.SkippedManifest != 2 {
		t.Errorf("incremental re-run: exported=%d skipped=%d, want 0/2", e2.Stats.Exported, e2.Stats.SkippedManifest)
	}

	e3 := fullExporter(out, manifest)
	for _, m := range []*model.Message{b, a} { // reversed order
		if _, err := e3.Export("store", []string{"Inbox"}, m); err != nil {
			t.Fatal(err)
		}
	}
	if names3 := htmlNames(t, out); names3 != names1 {
		t.Errorf("full re-run in reversed order renamed files:\n before %s\n after  %s", names1, names3)
	}
}

// covers: MA-86, R3, R1, S23
// The envelope fingerprint separates "a different message reused this
// Message-ID" from "the same message finally arrived in full": a message
// captured INCOMPLETE (empty attachment) is filled — same key — when its bytes
// arrive, while a different message (other subject) reusing the id while the
// first is still incomplete is kept as its own entry, never mistaken for the
// fill.
func TestFillVersusReuseWhileIncomplete(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	att := &blob{}
	first := func() *model.Message {
		return &model.Message{Subject: "Invoice", Received: testDate, InternetMessageID: "<inv@x>", PlainBody: "see attached",
			Attachments: []model.Attachment{att.att("inv.pdf")}}
	}
	e1 := incExporter(out, manifest)
	if _, err := e1.Export("store", []string{"Inbox"}, first()); err != nil {
		t.Fatal(err)
	}
	if manifest.Len() != 1 {
		t.Fatalf("manifest = %d", manifest.Len())
	}
	// A different message reusing the id, while the first is still incomplete.
	other := &model.Message{Subject: "Totally different", Received: testDate.Add(time.Hour), InternetMessageID: "<inv@x>", PlainBody: "other"}
	e2 := incExporter(out, manifest)
	if _, err := e2.Export("store", []string{"Inbox"}, other); err != nil {
		t.Fatal(err)
	}
	if e2.Stats.Exported != 1 || manifest.Len() != 2 || e2.Stats.Filled != 0 {
		t.Errorf("reuse while incomplete: exported=%d manifest=%d filled=%d, want 1/2/0", e2.Stats.Exported, manifest.Len(), e2.Stats.Filled)
	}
	// The first message's bytes arrive: it is filled under its own key.
	att.data = []byte("PDF")
	e3 := incExporter(out, manifest)
	if _, err := e3.Export("store", []string{"Inbox"}, first()); err != nil {
		t.Fatal(err)
	}
	if e3.Stats.Filled != 1 || manifest.Len() != 2 {
		t.Errorf("fill: filled=%d manifest=%d, want 1/2", e3.Stats.Filled, manifest.Len())
	}
}

// covers: MA-87, R4, R6, R13, S24
// A 32-bit stem collision between two different keys must never overwrite: the
// second stem is lengthened deterministically from its own key. Re-exporting a
// message whose record names a file from an older naming rule removes the
// previous html/zip (the message moves; it is never duplicated).
func TestStemCollisionAndRename(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)

	// Brute-force two Message-IDs whose manifest keys share an 8-hex ShortHash.
	id1, id2 := findHashCollision(t)
	m1 := &model.Message{Subject: "Same", Received: testDate, InternetMessageID: id1, PlainBody: "one"}
	m2 := &model.Message{Subject: "Same", Received: testDate, InternetMessageID: id2, PlainBody: "two"}
	e := incExporter(out, manifest)
	for _, m := range []*model.Message{m1, m2} {
		if _, err := e.Export("store", []string{"Inbox"}, m); err != nil {
			t.Fatal(err)
		}
	}
	if n := countSuffix(t, out, ".html"); n != 2 {
		t.Fatalf("stem collision overwrote a message: %d html files", n)
	}
	r1, _ := manifest.Get(state.Key("store", "Inbox", m1.Identity()))
	r2, _ := manifest.Get(state.Key("store", "Inbox", m2.Identity()))
	if r1.Path == r2.Path {
		t.Fatalf("both records point at %s", r1.Path)
	}
	// Deterministic: a full re-run in the opposite order keeps both paths.
	ef := fullExporter(out, manifest)
	for _, m := range []*model.Message{m2, m1} {
		if _, err := ef.Export("store", []string{"Inbox"}, m); err != nil {
			t.Fatal(err)
		}
	}
	if q1, _ := manifest.Get(state.Key("store", "Inbox", m1.Identity())); q1.Path != r1.Path {
		t.Errorf("re-run moved %s to %s", r1.Path, q1.Path)
	}
	if q2, _ := manifest.Get(state.Key("store", "Inbox", m2.Identity())); q2.Path != r2.Path {
		t.Errorf("re-run moved %s to %s", r2.Path, q2.Path)
	}

	// Rename: the record points at a file named by an older naming rule (the
	// stem rule changed between versions). Re-exporting moves the message: the
	// new file is written and the old one removed.
	k1 := state.Key("store", "Inbox", m1.Identity())
	rec1, _ := manifest.Get(k1)
	oldRel := "store/Inbox/old-rule-name.html"
	oldAbs := filepath.Join(out, filepath.FromSlash(oldRel))
	if err := os.WriteFile(oldAbs, []byte("<p>old</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strings.TrimSuffix(oldAbs, ".html")+"-attachments.zip", []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec1.Path = oldRel
	manifest.Add(k1, rec1)
	if _, err := ef.Export("store", []string{"Inbox"}, m1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldAbs); err == nil {
		t.Errorf("old file %s survived the rename", oldRel)
	}
	if _, err := os.Stat(strings.TrimSuffix(oldAbs, ".html") + "-attachments.zip"); err == nil {
		t.Error("old zip survived the rename")
	}
	if n := countSuffix(t, out, ".html"); n != 2 {
		t.Errorf("after rename: %d html files, want 2", n)
	}
	if moved, _ := manifest.Get(k1); moved.Path != r1.Path {
		t.Errorf("record path after rename = %s, want %s", moved.Path, r1.Path)
	}
}

func htmlNames(t *testing.T, out string) string {
	t.Helper()
	var names []string
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") {
			names = append(names, filepath.Base(p))
		}
		return nil
	})
	return strings.Join(names, ",")
}

// findHashCollision returns two Message-IDs whose Inbox-scoped manifest keys
// have the same ShortHash (8 hex = 32 bits): a birthday search over ~100k
// candidates, deterministic across runs.
func findHashCollision(t *testing.T) (string, string) {
	t.Helper()
	seen := map[string]string{}
	for i := 0; i < 2_000_000; i++ {
		id := fmt.Sprintf("<c%d@x>", i)
		key := state.Key("store", "Inbox", "mid:"+id) // exactly what Identity() yields for InternetMessageID=id
		h := util.ShortHash(key)
		if prev, ok := seen[h]; ok {
			return prev, id
		}
		seen[h] = id
	}
	// Unreachable in practice; make the failure legible if it ever is.
	sum := sha1.Sum([]byte("no collision"))
	t.Fatalf("no 32-bit collision found (%s)", hex.EncodeToString(sum[:4]))
	return "", ""
}
