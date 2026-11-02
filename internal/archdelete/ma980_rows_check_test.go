package archdelete

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
)

// plainRowArchive is a small plain archive whose manifest parses.
func plainRowArchive(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: "Mailbox/Inbox/one.html"}})
	writeFile(t, filepath.Join(dir, "Mailbox", "Inbox", "one.html"), "<p>one</p>")
	return dir
}

// covers: MA-980, R36
// A row does not offer Delete for a folder the plan always refuses (del-s3a-A2-02; design §10:
// Delete's enabled state comes from §4's test and §7, whose rows include the link and the
// protected folder): a row that is a link to an archive, and a row that holds another known
// archive, are refused on the row in the plan's own words and case. Positive twin first: the
// archive's own row is offered.
func TestARowRefusesALinkAndAProtectedFolderAsThePlanDoes(t *testing.T) {
	dir := plainRowArchive(t, "Mail")
	outer := plainRowArchive(t, "Outer")
	inner := filepath.Join(outer, "Inner")
	writeManifest(t, inner, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: "Mailbox/Inbox/two.html"}})
	link := filepath.Join(t.TempDir(), "MailLink")
	symlinkOrSkip(t, dir, link) // a folder: a junction on Windows, never skipped
	deps := testDeps()
	deps.Protected = DefaultProtected([]string{link, dir, outer, inner})

	if err := RowRefusal(dir, deps, false); err != nil {
		t.Fatalf("the positive twin: the archive's own row: %v", err)
	}
	for _, c := range []struct {
		row  string
		want Case
		says string
	}{
		{link, CaseLink, "is, or holds where its files are, a link"},
		{outer, CaseProtected, "MailArchive will not delete this folder: it is (or is inside, or holds) " + inner},
	} {
		err := RowRefusal(c.row, deps, true)
		r := AsRefusal(err)
		if r == nil || r.Case != c.want {
			t.Fatalf("the row %s: %v; want the %s refusal", c.row, err, c.want)
		}
		assure.Refused(t, 1, r.Message, assure.Code(r.ExitCode()), assure.Names(c.says),
			assure.NoSideEffect(func() bool { return exists(filepath.Join(c.row, manifestName)) }))
		if _, msg, pc := refusalOf(t, c.row, deps); pc != c.want || msg != r.Message {
			t.Errorf("the plan of %s says %s %q; the row %s %q", c.row, pc, msg, r.Case, r.Message)
		}
	}
}

// countStats counts the file system asks of each path under prefix until the test ends.
func countStats(t *testing.T, prefix string) map[string]int {
	t.Helper()
	n := map[string]int{}
	old := statSeam
	t.Cleanup(func() { statSeam = old })
	statSeam = func(p string) (os.FileInfo, error) {
		if strings.HasPrefix(p, prefix) {
			n[p]++
		}
		return old(p)
	}
	return n
}

// covers: MA-980, R36
// A page of rows reads deletions.log once and asks the file system nothing for a delete that
// finished (del-s3a-A3-04, A4-05): a folder logged long ago on a network share, offline since,
// is not asked at all on a page load while every delete in the log finished, and once a delete
// in the log did not finish, it is asked once per page whatever the number of rows. Positive
// twin first: a delete that did not finish is still found for its row.
func TestARowsCheckAsksTheFileSystemOnlyForAnUnfinishedDeleteAndOnce(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), LogName)
	offline := filepath.Join(t.TempDir(), "offline-share")
	var rows []string
	for i := 0; i < 4; i++ {
		rows = append(rows, plainRowArchive(t, "Row"+string(rune('A'+i))))
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	for i, run := range []string{"r1", "r2", "r3"} {
		gone := filepath.Join(offline, "Mail"+string(rune('1'+i)))
		appendRawLog(t, logPath, map[string]any{"at": at, "event": "started", "run": run, "folder": gone, "row": gone, "kind": "plain"})
		appendRawLog(t, logPath, map[string]any{"at": at, "event": "finished", "run": run, "folder": gone, "row": gone, "outcome": "complete"})
	}
	deps := testDeps()
	deps.Log = logPath

	// The positive twin: a delete of rows[0] that did not finish (a started line alone).
	appendRawLog(t, logPath, map[string]any{"at": at, "event": "started", "run": "r9", "folder": rows[0], "row": rows[0], "kind": "plain"})
	if u, _ := NewRowsCheck(deps).State(rows[0], true); u == nil {
		t.Fatal("the positive twin: the unfinished delete of the row is not found")
	}
	appendRawLog(t, logPath, map[string]any{"at": at, "event": "finished", "run": "r9", "folder": rows[0], "row": rows[0], "outcome": "complete"})

	asked := countStats(t, offline)
	rc := NewRowsCheck(deps)
	for _, r := range rows {
		if u, err := rc.State(r, true); u != nil || err != nil {
			t.Fatalf("the row %s: unfinished %v, %v", r, u, err)
		}
	}
	if len(asked) != 0 {
		t.Fatalf("every delete in the log finished, yet the page asked the offline share: %v", asked)
	}

	// A delete of an offline folder that did not finish: each logged folder is asked once for
	// the page, whatever the number of rows.
	gone := filepath.Join(offline, "Mail9")
	appendRawLog(t, logPath, map[string]any{"at": at, "event": "started", "run": "r10", "folder": gone, "row": gone, "kind": "plain"})
	rc = NewRowsCheck(deps)
	for _, r := range rows {
		if u, _ := rc.State(r, true); u != nil {
			t.Fatalf("the row %s is not the offline folder: %v", r, u)
		}
	}
	if asked[gone] == 0 {
		t.Fatal("the unfinished delete's folder was never compared with the rows")
	}
	for p, n := range asked {
		if n != 1 {
			t.Errorf("%s was asked %d times for one page of %d rows; want once", p, n, len(rows))
		}
	}
}

// covers: MA-980, R36
// A row keeps its manifest's parse while the manifest is unchanged (del-s3a-A3-03): a manifest
// over archindex's 1 MiB read is not parsed again on every page load. Kept by its identity,
// size and modification time: a manifest that changed is parsed again and its refusal shown.
// Positive twin first: the row's archive test passes. (The parse is shown kept by bytes that do
// not parse written in place at the same size, the modification time put back: what the row
// keeps is then what it says.)
func TestARowKeepsItsManifestsParseUntilTheManifestChanges(t *testing.T) {
	dir := plainRowArchive(t, "Kept")
	man := filepath.Join(dir, manifestName)
	deps := testDeps()
	if err := RowRefusal(dir, deps, false); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	fi, err := os.Stat(man)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] = ' ' // the closing brace gone: the same size, no longer parses
	f, err := os.OpenFile(man, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Chtimes(man, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := RowRefusal(dir, deps, false); err != nil {
		t.Fatalf("the unchanged manifest (identity, size, time) was parsed again: %v", err)
	}
	later := fi.ModTime().Add(time.Minute)
	if err := os.Chtimes(man, later, later); err != nil {
		t.Fatal(err)
	}
	r := AsRefusal(RowRefusal(dir, deps, false))
	if r == nil || r.Case != CaseNotArchive {
		t.Fatalf("a changed manifest that does not parse: %v; want the not-an-archive refusal", r)
	}
	assure.Refused(t, 1, r.Message, assure.Code(r.ExitCode()), assure.Names(msgNotArchive),
		assure.NoSideEffect(func() bool { return exists(man) }))
}

// covers: MA-980, R36
// A known archive this computer's record names only in its folded spelling (encrecord.Spelling:
// Unicode's full case folding, ß as ss), which NTFS, folding letter by letter, does not open
// (del-s3a-A1-03), still refuses a delete of a folder that holds it or is inside it: compared by
// that spelling, found name by name. A spelling no folder has now refuses nothing. The file
// system's refusal of the folded spelling is stood in for by the stat seam. Positive twin first:
// with no record, the folder is not protected.
func TestAKnownArchiveRecordedByItsFoldedSpellingIsStillProtected(t *testing.T) {
	outer := filepath.Join(t.TempDir(), "Users")
	vault := filepath.Join(outer, "Weiß", "Mail", "Vault")
	if err := os.MkdirAll(filepath.Join(vault, "Sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	spelling := encrecord.Spelling(vault)
	// On Linux the record keeps the exact spelling (no folding), which the file system opens: there
	// is nothing to stand in for, and the record's own spelling is what protects.
	ntfs := func(p string) (os.FileInfo, error) {
		if p == spelling && spelling != vault {
			return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
		}
		return os.Stat(p)
	}
	p := Protected{}
	if err := checkProtectedStat(ntfs, outer, p); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	p.Archives = []string{spelling}
	for _, folder := range []string{outer, filepath.Join(vault, "Sub")} {
		err := checkProtectedStat(ntfs, folder, p)
		r := AsRefusal(err)
		if r == nil || r.Case != CaseProtected {
			t.Fatalf("a delete of %s beside the recorded %s: %v; want the protected refusal", folder, spelling, err)
		}
		assure.Refused(t, 1, r.Message, assure.Code(r.ExitCode()), assure.Names(spelling),
			assure.NoSideEffect(func() bool { return exists(vault) }))
	}
	if err := checkProtectedStat(ntfs, vault, p); err != nil {
		t.Errorf("the recorded archive's own folder is not another archive: %v", err)
	}
	p.Archives = []string{encrecord.Spelling(filepath.Join(outer, "Gone", "Vault"))}
	if err := checkProtectedStat(ntfs, outer, p); err != nil {
		t.Errorf("a spelling no folder has now: %v", err)
	}
}

// covers: MA-981, R36
// A Forget of the encrypted archive's record between the plan and the delete refuses the delete
// at its re-check under the lock (del-s3a-A2-01): the plan's record is compared, so step 8 is
// never reached with a record that is gone and the result never says this computer still
// remembers a key it forgot. Nothing is removed. Positive twin first: the record unchanged, the
// delete completes and the record is forgotten.
func TestARecordForgottenSincePlanRefusesTheDelete(t *testing.T) {
	twin := encRig(t, encryptedClean(t))
	if res := twin.run(t, false); res.Status != StatusComplete || len(twin.kr.forgot) != 1 {
		t.Fatalf("the positive twin: %s (forgot %v)", res.Message(), twin.kr.forgot)
	}

	dir := encryptedClean(t)
	r := newRig(t, dir)
	recorded := true
	r.x.Deps.Record = func(f string) (RecordRef, error) {
		if !recorded {
			return RecordRef{}, nil
		}
		return RecordRef{Recorded: true, ID: testArchiveID, Spelling: f}, nil
	}
	p := r.plan(t)
	before := tree(t, dir)
	recorded = false // Forget this encrypted archive here, in another tab
	res := Execute(p, p.Name, false, r.x)
	if res.Refusal == nil || res.Refusal.Case != CaseChanged {
		t.Fatalf("the delete after the record was forgotten: %s %v", res.Status, res.Lines)
	}
	// The archive lock taken at step 1 (a) writes its owner into its own file, as every lock does.
	after := tree(t, dir)
	delete(after, lockName)
	delete(before, lockName)
	assure.Refused(t, 1, res.Message(), assure.Code(res.Refusal.ExitCode()), assure.Names(res.Refusal.Message),
		assure.NoSideEffect(func() bool { return sameTree(after, before) && len(r.kr.asked) == 0 && len(r.rows) == 0 }))
	if strings.Contains(strings.Join(res.Lines, "\n"), "still remembers") {
		t.Errorf("the result says the key is still remembered: %v", res.Lines)
	}
}
