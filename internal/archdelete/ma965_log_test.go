package archdelete

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// recordDigest is the hex on a record's last line, and the SHA-256 of every byte
// before that line.
func recordDigest(t *testing.T, rec []byte) (stated, computed string) {
	t.Helper()
	i := bytes.LastIndex(rec[:len(rec)-1], []byte("\n"))
	last := string(rec[i+1:])
	if !strings.HasPrefix(last, "Record-SHA256: ") {
		t.Fatalf("the record's last line is %q", last)
	}
	sum := sha256.Sum256(rec[:i+1])
	return strings.TrimSpace(strings.TrimPrefix(last, "Record-SHA256: ")), hex.EncodeToString(sum[:])
}

// covers: MA-965, R36
func TestDeletionsLogKeepsStartedFolderAndFinishedLines(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	res := r.run(t, false)
	if res.Status != StatusComplete || !res.FolderGone || !hasLine(res, "The folder is gone.") {
		t.Fatalf("%s", res.Message())
	}
	lines := assure.Reached(t, logLines(t, r.x.Log), "deletions.log's lines")
	if ev := strings.Join(events(lines), ","); ev != "started,folder removed,finished complete" {
		t.Fatalf("events: %s", ev)
	}
	s := lines[0]
	if s["folder"] != dir || s["kind"] != "plain" || s["delete_unrecorded"] != "no" || s["files"] == nil || s["bytes"] == nil || s["at"] == nil {
		t.Fatalf("started line: %v", s)
	}
	stated, computed := recordDigest(t, res.Record)
	if stated != computed || lines[2]["record_sha256"] != stated {
		t.Fatalf("the finished line's digest %v, the record's %s, recomputed %s", lines[2]["record_sha256"], stated, computed)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(r.x.Log)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("deletions.log must be owner-only: %v %v", fi.Mode(), err)
		}
	}

	// Ticked §4.3 copies: the choice is in the started line and the record.
	d2 := plainBesidePersonsFiles(t)
	writeFile(t, filepath.Join(d2, "Mailbox", "Inbox", "2020-01-01_0000_old_00aa00aa.html"), "an older copy")
	r2 := newRig(t, d2)
	res = r2.run(t, true)
	if l := logLines(t, r2.x.Log); l[0]["delete_unrecorded"] != "yes" || l[0]["unrecorded"] != float64(1) {
		t.Fatalf("started line: %v", l[0])
	}
	if !strings.Contains(string(res.Record), "Mail copies the archive did not record: 1; delete them too: yes\n") {
		t.Fatalf("record:\n%s", res.Record)
	}
}

// covers: MA-965, R36
func TestTheStartedLineNamesAnEncryptedArchivesRecord(t *testing.T) {
	dir := encryptedClean(t)
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	if res := r.run(t, false); res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	s := logLines(t, r.x.Log)[0]
	if s["kind"] != "encrypted" || s["record_id"] != testArchiveID || s["record_spelling"] != dir || s["not_recorded"] == true {
		t.Fatalf("started line: %v", s)
	}
	// Not recorded on this computer: the line says so.
	d2 := encryptedClean(t)
	r2 := newRig(t, d2)
	if res := r2.run(t, false); res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	if s := logLines(t, r2.x.Log)[0]; s["not_recorded"] != true || s["record_id"] != nil {
		t.Fatalf("started line: %v", s)
	}
}

// covers: MA-965, R36
func TestDeletionsLogIsAppendedOwnerOnlyAndNeverThroughALink(t *testing.T) {
	r := newRig(t, smallArchive(t, t.TempDir()))
	prior := `{"at":"2020-01-01T00:00:00Z","event":"started","folder":"/elsewhere"}` + "\n"
	writeFile(t, r.x.Log, prior)
	if err := os.Chmod(r.x.Log, 0o644); err != nil {
		t.Fatal(err)
	}
	if res := r.run(t, false); res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	b, err := os.ReadFile(r.x.Log)
	if err != nil || !strings.HasPrefix(string(b), prior) || len(b) == len(prior) {
		t.Fatalf("the log is appended to, its earlier lines kept: %q", b)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(r.x.Log); fi.Mode().Perm() != 0o600 {
			t.Fatalf("an existing log is made owner-only: %v", fi.Mode())
		}
	}

	// A log that is a link: step 0 refuses, nothing is deleted, the link's target is
	// never written.
	r2 := newRig(t, smallArchive(t, t.TempDir()))
	target := filepath.Join(t.TempDir(), "elsewhere.txt")
	writeFile(t, target, "someone else's file")
	symlinkOrSkip(t, target, r2.x.Log)
	before := tree(t, r2.dir)
	res := r2.run(t, false)
	assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(1), assure.Names(msgLogFailed),
		assure.NoSideEffect(func() bool {
			b, _ := os.ReadFile(target)
			return sameTree(before, tree(t, r2.dir)) && string(b) == "someone else's file"
		}))
}
