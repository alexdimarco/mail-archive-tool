package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// fixtureMsg describes one message to write into a test archive.
type fixtureMsg struct {
	subject string
	attach  []byte // non-nil → one attachment with these bytes (produces a zip)
	raw     []byte // non-nil → keep the original bytes as an .eml
}

// buildFixityArchive writes msgs into a real archive at out via the exporter (so the
// digests are recorded exactly as a production run records them) and saves the
// manifest at the standard path verify reads.
func buildFixityArchive(t *testing.T, out string, msgs ...fixtureMsg) {
	t.Helper()
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	keepRaw := false
	for _, fm := range msgs {
		if fm.raw != nil {
			keepRaw = true
		}
	}
	exp := &export.Exporter{OutDir: out, Manifest: m, Log: log.New(io.Discard, "", 0), KeepRaw: keepRaw}
	for i, fm := range msgs {
		msg := &model.Message{
			Subject:           fm.subject,
			SenderEmail:       "a@example.com",
			Received:          time.Date(2025, 3, 3, 9, i%60, 0, 0, time.UTC),
			InternetMessageID: "<m" + itoaTest(i) + "@x>",
			HTMLBody:          "<p>" + fm.subject + "</p>",
		}
		if fm.attach != nil {
			b := fm.attach
			msg.Attachments = []model.Attachment{{Filename: "a.bin", WriteTo: func(w io.Writer) (int64, error) {
				n, _ := w.Write(b)
				return int64(n), nil
			}}}
		}
		if fm.raw != nil {
			msg.Raw = fm.raw
		}
		if _, err := exp.Export("store", []string{"Inbox"}, msg); err != nil {
			t.Fatalf("export %q: %v", fm.subject, err)
		}
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// findSuffix returns every path under root (relative, forward-slashed) whose
// base name ends with suffix and is not the folder index.html page.
func findSuffix(t *testing.T, root, suffix string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == "index.html" {
			return nil
		}
		if strings.HasSuffix(name, suffix) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func discard() *log.Logger { return log.New(io.Discard, "", 0) }

// covers: MA-135, R1, R5, S31
// The exporter records a sha256+length for every file it writes — the html
// always, the zip and eml when present — at write time, and a fresh archive
// verifies as attested (exit 0). The -json document carries the coverage
// fields records/with_fixity/checked. Positive twin for the whole slice: a
// healthy, freshly-written archive attests.
func TestVerifyFreshArchiveAttested(t *testing.T) {
	out := tmpDir(t)
	buildFixityArchive(t,
		out,
		fixtureMsg{subject: "with attach and raw", attach: []byte("attachment-bytes"), raw: []byte("From: a@x\r\n\r\nraw body\r\n")},
		fixtureMsg{subject: "plain body only"},
	)

	// Write-time fixity is on the records themselves.
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	sawAll := false
	for _, r := range assure.Reached(t, m.All(), "manifest records") {
		if r.Fixity == nil || r.Fixity.HTML == nil {
			t.Fatalf("record %q has no recorded html digest: %+v", r.Path, r.Fixity)
		}
		if r.Fixity.Zip != nil && r.Fixity.EML != nil {
			sawAll = true
		}
	}
	if !sawAll {
		t.Fatal("the attachment+raw message did not record all three (html, zip, eml) digests")
	}

	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify of a fresh archive refused: %v", err)
	}
	if !rep.Attested() || rep.ExitCode() != 0 {
		t.Fatalf("fresh archive not attested: exit=%d %+v", rep.ExitCode(), rep)
	}
	if rep.Modified+rep.Missing+rep.Unrecorded != 0 {
		t.Fatalf("fresh archive had problems: %+v", rep)
	}
	if rep.Records != 2 || rep.WithFixity != 2 {
		t.Fatalf("coverage wrong: records=%d with_fixity=%d", rep.Records, rep.WithFixity)
	}
	if rep.Checked < 4 { // html+zip+eml for the first, html for the second
		t.Fatalf("checked too few files: %d", rep.Checked)
	}

	// -json carries the coverage fields and the problem list.
	js, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"records"`, `"with_fixity"`, `"checked"`, `"problems"`} {
		if !strings.Contains(string(js), key) {
			t.Errorf("verify -json is missing the %s field:\n%s", key, js)
		}
	}
}

// covers: MA-136, R1, R5, S31
// A flipped byte in an archived file is reported modified (naming the path), and
// a deleted zip is reported missing — each making verify not attested (exit 2).
// The positive twin: the same archive attested before the damage.
func TestVerifyModifiedAndMissing(t *testing.T) {
	out := tmpDir(t)
	buildFixityArchive(t,
		out,
		fixtureMsg{subject: "has attachment", attach: []byte("original-attachment")},
		fixtureMsg{subject: "plain"},
	)

	// Positive twin.
	if rep, err := Verify(out, VerifyOptions{}, discard(), nil); err != nil || !rep.Attested() {
		t.Fatalf("intact archive not attested: err=%v %+v", err, rep)
	}

	// Flip a byte of one html in place (same length → sha mismatch).
	htmls := assure.Reached(t, findSuffix(t, out, ".html"), "archived html files")
	victim := htmls[0]
	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(victim, data, 0o644); err != nil {
		t.Fatal(err)
	}
	victimRel, _ := filepath.Rel(out, victim)
	victimRel = filepath.ToSlash(victimRel)

	// Delete the zip.
	zips := assure.Reached(t, findSuffix(t, out, "-attachments.zip"), "archived zips")
	if err := os.Remove(zips[0]); err != nil {
		t.Fatal(err)
	}

	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify refused after damage: %v", err)
	}
	if rep.ExitCode() != 2 || rep.Attested() {
		t.Fatalf("damaged archive still attested: exit=%d %+v", rep.ExitCode(), rep)
	}
	if rep.Modified < 1 || rep.Missing < 1 {
		t.Fatalf("expected a modified and a missing file: %+v", rep)
	}
	if !hasProblem(rep, "modified", victimRel) {
		t.Errorf("modified file not named: want %q in %+v", victimRel, rep.Problems)
	}
	if !anyProblem(rep, "missing") {
		t.Errorf("deleted zip not reported missing: %+v", rep.Problems)
	}
}

// covers: MA-137, R1, R5, S31
// A record that carries no recorded fixity (a legacy archive, or files written
// before verify -record baselined them) is reported unrecorded, exit 2, and the
// summary names `verify -record`; `verify -record` then baselines the current
// bytes and a following verify exits 0. Positive twin: the archive attests once
// baselined.
func TestVerifyLegacyUnrecordedThenRecord(t *testing.T) {
	out := tmpDir(t)
	buildFixityArchive(t, out, fixtureMsg{subject: "one"}, fixtureMsg{subject: "two"})

	// Strip fixity from every record to simulate an archive written before it.
	stripFixity(t, out)

	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify refused: %v", err)
	}
	if rep.ExitCode() != 2 || rep.Unrecorded < 2 {
		t.Fatalf("legacy archive should be not-attested with unrecorded files: %+v", rep)
	}
	if !strings.Contains(strings.Join(VerifySummary(rep), "\n"), "verify -record") {
		t.Errorf("summary does not name the `verify -record` remedy:\n%s", strings.Join(VerifySummary(rep), "\n"))
	}

	// Baseline with -record.
	recRep, err := Verify(out, VerifyOptions{Record: true}, discard(), nil)
	if err != nil {
		t.Fatalf("verify -record refused: %v", err)
	}
	if recRep.Recorded < 2 || recRep.Unrecorded != 0 || recRep.ExitCode() != 0 {
		t.Fatalf("verify -record did not baseline everything: %+v", recRep)
	}

	// A following plain verify now attests, reading the baselined digests.
	after, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify after -record refused: %v", err)
	}
	if !after.Attested() || after.ExitCode() != 0 || after.WithFixity != after.Records {
		t.Fatalf("archive not attested after baseline: %+v", after)
	}
}

// covers: MA-138, R1, S31
// A stray html/zip/eml under a store directory that no record owns is reported
// unexpected and does not change the exit (an attested archive with a stray
// file still exits 0); the tool's own files (folder index.html, README.txt) are
// never flagged.
func TestVerifyUnexpectedStrayFile(t *testing.T) {
	out := tmpDir(t)
	buildFixityArchive(t, out, fixtureMsg{subject: "kept"})

	dir := filepath.Join(out, "store", "Inbox")
	writeFile(t, filepath.Join(dir, "orphan.html"), "<p>stray</p>")
	// Tool-owned files that must NOT be flagged.
	writeFile(t, filepath.Join(dir, "index.html"), "<p>folder page</p>")
	writeFile(t, filepath.Join(out, "README.txt"), "layout")

	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify refused: %v", err)
	}
	if rep.Unexpected != 1 || !hasProblem(rep, "unexpected", "store/Inbox/orphan.html") {
		t.Fatalf("stray file not reported as the single unexpected: %+v", rep)
	}
	if !rep.Attested() || rep.ExitCode() != 0 {
		t.Fatalf("an unexpected file must not change the exit (still attested): exit=%d %+v", rep.ExitCode(), rep)
	}
}

// covers: MA-139, R4, R5, S31
// verify trusts nothing in a manifest path: a ".." path and an absolute path are
// integrity failures classified without touching the filesystem; a symlinked
// directory component is a modification (the symlink is never followed — even
// when its target would match the recorded digest, proving the target is never
// read); a FIFO is classified without being opened; and an oversized file stops
// reading at the recorded size + 1 byte.
func TestVerifyContainmentAndBounds(t *testing.T) {
	out := tmpDir(t)
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	add := func(key, path string, d state.FileDigest) {
		m.Add(key, state.Record{Path: path, Fixity: &state.Fixity{HTML: &d}})
	}

	// A symlinked directory component pointing OUTSIDE the archive at a real
	// file whose bytes match the recorded digest: if verify followed it, the
	// file would read as ok. It must instead be modified (symlink not followed).
	external := tmpDir(t)
	if err := os.MkdirAll(filepath.Join(external, "Inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := []byte("secret outside the archive")
	if err := os.WriteFile(filepath.Join(external, "Inbox", "foo.html"), target, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(out, "link")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	add("k-link", "link/Inbox/foo.html", state.FileDigest{SHA256: sha256hex(target), Size: int64(len(target))})

	// A ".." path and an absolute path — rejected before any filesystem access.
	add("k-dotdot", "store/../escape.html", state.FileDigest{SHA256: sha256hex([]byte("x")), Size: 1})
	add("k-abs", "/etc/passwd", state.FileDigest{SHA256: sha256hex([]byte("x")), Size: 1})

	// An oversized file: recorded 5 bytes, actually 100000 — must be modified
	// (size), and the read must stop at size+1.
	inbox := filepath.Join(out, "store", "Inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(inbox, "big.html")
	if err := os.WriteFile(big, make([]byte, 100000), 0o644); err != nil {
		t.Fatal(err)
	}
	add("k-big", "store/Inbox/big.html", state.FileDigest{SHA256: sha256hex(make([]byte, 5)), Size: 5})

	// A FIFO where a regular file should be (unix only).
	haveFIFO := makeFIFO(t, filepath.Join(inbox, "fifo.html"))
	if haveFIFO {
		add("k-fifo", "store/Inbox/fifo.html", state.FileDigest{SHA256: sha256hex([]byte("x")), Size: 1})
	}

	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify refused: %v", err)
	}
	// None of these may read as ok; every one is an integrity failure.
	if rep.OK != 0 {
		t.Fatalf("a tampered/irregular file read as ok — containment breached: %+v", rep)
	}
	// dotdot(1) + absolute(1) + oversized(1) + a symlinked parent flagging the
	// html and both derived siblings (3) = 6, plus the FIFO (1) on unix.
	wantModified := 6
	if haveFIFO {
		wantModified = 7
	}
	if rep.Modified != wantModified {
		t.Fatalf("modified=%d, want %d: %+v", rep.Modified, wantModified, rep)
	}
	if p := problemFor(rep, "link/Inbox/foo.html"); p == nil || !strings.Contains(p.Detail, "symlink") {
		t.Errorf("symlinked component not reported as a non-followed symlink: %+v", rep.Problems)
	}
	if p := problemFor(rep, "store/Inbox/big.html"); p == nil || p.Kind != "modified" || !strings.Contains(p.Detail, "size") {
		t.Errorf("oversized file not reported as a size mismatch: %+v", rep.Problems)
	}
	if !anyProblem(rep, "modified") || !hasRawPathProblem(rep, "escape.html") || !hasRawPathProblem(rep, "/etc/passwd") {
		t.Errorf("a bad ..-path or absolute path was not rejected: %+v", rep.Problems)
	}
	if haveFIFO {
		if p := problemFor(rep, "store/Inbox/fifo.html"); p == nil || !strings.Contains(p.Detail, "regular file") {
			t.Errorf("FIFO not reported as a non-regular file: %+v", rep.Problems)
		}
	}

	// The read cap is deterministic: hashing the big file with limit size+1
	// stops at exactly size+1 bytes, never the whole 100000.
	if _, n, err := hashFile(big, 6); err != nil || n != 6 {
		t.Errorf("read cap not honoured: hashFile(limit=6) read %d bytes (err=%v), want 6", n, err)
	}
}

// covers: MA-140, R4, R5, R12, S25, S31
// verify writes nothing to the manifest without -record (its bytes are byte-for-
// byte unchanged), and refuses a locked archive with a typed refusal naming the
// lock file and the verb holding it; the holder line a verify run writes names
// itself. Positive twin: verify succeeds once the lock is released.
func TestVerifyNoSideEffectAndLock(t *testing.T) {
	out := tmpDir(t)
	buildFixityArchive(t, out, fixtureMsg{subject: "one"}, fixtureMsg{subject: "two"})
	mpath := filepath.Join(out, ".mailarchive-manifest.json")

	// Strip fixity so there IS something verify could baseline: a plain verify
	// must still write nothing (the guard is meaningless on an already-recorded
	// archive, where there is nothing to write either way).
	stripFixity(t, out)
	before, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(out, VerifyOptions{}, discard(), nil); err != nil {
		t.Fatalf("verify refused an archive: %v", err)
	}
	after, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("verify without -record rewrote the manifest (fail-dirty)")
	}

	// A run holding the lock names its verb; verify refuses, naming it.
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	_, verr := Verify(out, VerifyOptions{}, discard(), nil)
	code, msg := 0, ""
	if verr != nil {
		code, msg = 1, verr.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("in use", lockfile.Name, "export"),
		assure.NoSideEffect(func() bool {
			now, e := os.ReadFile(mpath)
			return e == nil && string(now) == string(before)
		}))
	held.Release()

	// FC5: a verify run's own holder line names "verify", so a refusal against
	// it reads "held by mailarchive verify …".
	vheld, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "verify")
	if err != nil {
		t.Fatal(err)
	}
	_, again := lockfile.Acquire(filepath.Join(out, lockfile.Name))
	if again == nil || !strings.Contains(again.Error(), "verify") {
		t.Errorf("a verify holder is not named in the lock refusal: %v", again)
	}
	vheld.Release()

	// Positive twin: once released, verify runs (returns a report, no refusal).
	if _, err := Verify(out, VerifyOptions{}, discard(), nil); err != nil {
		t.Fatalf("verify after release still refused: %v", err)
	}
}

// covers: MA-141, R1, S31
// Each per-category detail list is bounded: with the cap shrunk to 2, five
// unrecorded files yield an exact Unrecorded count of 5 but only 2 listed
// problems and a truncation note of 3, and the summary shows "list truncated".
func TestVerifyReportCapped(t *testing.T) {
	old := maxVerifyDetail
	maxVerifyDetail = 2
	defer func() { maxVerifyDetail = old }()

	out := tmpDir(t)
	buildFixityArchive(t, out,
		fixtureMsg{subject: "a"}, fixtureMsg{subject: "b"}, fixtureMsg{subject: "c"},
		fixtureMsg{subject: "d"}, fixtureMsg{subject: "e"},
	)
	stripFixity(t, out) // all five html become unrecorded

	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify refused: %v", err)
	}
	if rep.Unrecorded != 5 {
		t.Fatalf("exact count lost: Unrecorded=%d, want 5", rep.Unrecorded)
	}
	shown := 0
	for _, p := range rep.Problems {
		if p.Kind == "unrecorded" {
			shown++
		}
	}
	if shown != 2 || rep.Truncated["unrecorded"] != 3 {
		t.Fatalf("cap not applied: shown=%d truncated=%d", shown, rep.Truncated["unrecorded"])
	}
	if !strings.Contains(strings.Join(VerifySummary(rep), "\n"), "list truncated") {
		t.Errorf("summary lacks the truncation note:\n%s", strings.Join(VerifySummary(rep), "\n"))
	}
}

// ---- helpers ----

func stripFixity(t *testing.T, out string) {
	t.Helper()
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	for k, r := range m.All() {
		r.Fixity = nil
		m.Add(k, r)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasProblem(r Report, kind, path string) bool {
	for _, p := range r.Problems {
		if p.Kind == kind && p.Path == path {
			return true
		}
	}
	return false
}

func anyProblem(r Report, kind string) bool {
	for _, p := range r.Problems {
		if p.Kind == kind {
			return true
		}
	}
	return false
}

func problemFor(r Report, path string) *Problem {
	for i := range r.Problems {
		if r.Problems[i].Path == path {
			return &r.Problems[i]
		}
	}
	return nil
}

func hasRawPathProblem(r Report, needle string) bool {
	for _, p := range r.Problems {
		if p.Kind == "modified" && strings.Contains(p.Path, needle) {
			return true
		}
	}
	return false
}
