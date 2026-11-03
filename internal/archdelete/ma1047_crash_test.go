package archdelete

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/vault"
)

// A real crash at each of step 6's boundaries, and what Delete again does after it
// (docs/design-archive-delete.md §15.2's crash table, with the owner's ruling Q1 and DM-9; the
// slice-3 adversarial pass's probes, kept as tests; MA-1046, MA-1047). The crash is a real one: a
// child process running the real delete exits on the spot (os.Exit: no deferred cleanup, no
// finished line, the archive lock dropped by the system), as a power cut or a killed program would.

// crashChildEnv marks the re-executed test binary that runs the delete and crashes; its value is
// the boundary. crashDirEnv, crashLogEnv and crashEncEnv carry the folder, this computer's
// deletion log and "1" for an encrypted archive.
const (
	crashChildEnv = "MAILARCHIVE_TEST_DELETE_CRASH_AT"
	crashDirEnv   = "MAILARCHIVE_TEST_DELETE_CRASH_DIR"
	crashLogEnv   = "MAILARCHIVE_TEST_DELETE_CRASH_LOG"
	crashEncEnv   = "MAILARCHIVE_TEST_DELETE_CRASH_ENC"
	crashExit     = 7
)

// crashChild is the child's part: the real delete of the folder, killed at the boundary at:
//   - "create": the marker's name created, its bytes not yet written (an empty marker);
//   - "first": at step 6's first removal, the marker and its `marked` line written;
//   - "mid": at the second removal, one file gone;
//   - "anchor": right after the anchor (the manifest, or the vault file) was removed;
//   - "marker": right after the marker was removed.
//
// A delete that runs to its end without crashing exits with another code, which fails the parent.
func crashChild(t *testing.T, at string) {
	dir, logPath, enc := os.Getenv(crashDirEnv), os.Getenv(crashLogEnv), os.Getenv(crashEncEnv) == "1"
	var r *rig
	anchor := manifestName
	if enc {
		r, anchor = encRig(t, dir), vault.FileName
	} else {
		r = newRig(t, dir)
	}
	r.x.Log, r.x.Deps.Log = logPath, logPath
	calls := 0
	orig := removeFileFn
	removeFileFn = func(root *os.Root, rel string) error {
		calls++
		rel = filepath.ToSlash(rel)
		switch {
		case at == "first" && calls == 1, at == "mid" && calls == 2:
			os.Exit(crashExit)
		case at == "anchor" && rel == anchor, at == "marker" && rel == delmark.Name:
			if err := orig(root, rel); err != nil {
				fmt.Fprintln(os.Stderr, "the removal before the crash failed:", err)
				os.Exit(crashExit + 1)
			}
			os.Exit(crashExit)
		}
		return orig(root, rel)
	}
	if at == "create" {
		markerWrite = func(*os.File, []byte) error { os.Exit(crashExit); return nil }
	}
	res := r.run(t, false)
	fmt.Fprintln(os.Stderr, "the delete did not crash at", at+":", res.Status, res.Message())
	os.Exit(crashExit + 2)
}

// crashAt runs the real delete of dir with this computer's log at logPath in a child process and
// kills it at the boundary at.
func crashAt(t *testing.T, at, dir, logPath string, enc bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMA1047_ARealCrashAtEachStep6BoundaryLeavesWhatTheCrashTableSays$", "-test.count=1")
	tmp := t.TempDir() // the child's own temporary folders, removed with the parent's
	cmd.Env = append(os.Environ(), crashChildEnv+"="+at, crashDirEnv+"="+dir, crashLogEnv+"="+logPath, "TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)
	if enc {
		cmd.Env = append(cmd.Env, crashEncEnv+"=1")
	}
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != crashExit {
		t.Fatalf("the child did not crash at %s: %v\n%s", at, err, out)
	}
}

// covers: MA-1047, MA-1046, R36
// At every boundary of step 6, plain and encrypted, a real crash leaves what §15.2's crash table
// says, and Delete again on this computer finishes:
//   - the name created, its bytes not written: an empty marker, the whole archive;
//   - at the first removal: the marker and its `marked` line, the whole archive;
//   - between two removals; right after the anchor: the marker there;
//   - in each, another computer's backup (its own log never saw the delete) is refused in the
//     marker-only words, nothing written into the folder; Delete again ends complete, no marker
//     left, the person's files kept (an encrypted archive's key forgotten and its row removed);
//   - right after the anchor, Delete again is the no-anchor re-run (DM-9, case (ii)), and the folder
//     holds only what a complete delete keeps: the archive's emptied folders went before the anchor
//     (the slice-3 pass's MED-1), so none is left or called "not the archive's"; an archive beside
//     none of the person's files ends with its folder gone;
//   - right after the marker's removal (the stated open limit, §15.6, §15.7's DM-9 correction): no
//     marker, and Delete again creates none and is never complete (§5.4).
func TestMA1047_ARealCrashAtEachStep6BoundaryLeavesWhatTheCrashTableSays(t *testing.T) {
	if at := os.Getenv(crashChildEnv); at != "" {
		crashChild(t, at)
		return
	}
	type check func(t *testing.T, dir string, r *rig, enc bool)
	whole := func(t *testing.T, dir string, r *rig, enc bool) {
		if !IsArchive(dir) {
			t.Fatal("nothing was removed: the archive is whole")
		}
	}
	anchorGone := func(t *testing.T, dir string, r *rig, enc bool) {
		anchor := manifestName
		if enc {
			anchor = vault.FileName
		}
		if exists(filepath.Join(dir, anchor)) {
			t.Fatal("the anchor was removed before the crash")
		}
	}
	for _, c := range []struct {
		at        string
		marker    bool
		emptyMark bool
		marked    int // `marked` lines after the crash
		at6       check
	}{
		{"create", true, true, 0, whole},
		{"first", true, false, 1, whole},
		{"mid", true, false, 1, nil},
		{"anchor", true, false, 1, anchorGone},
		{"marker", false, false, 1, anchorGone},
	} {
		for _, enc := range []bool{false, true} {
			name := c.at
			if enc {
				name = "encrypted " + c.at
			}
			t.Run(name, func(t *testing.T) {
				var dir string
				var r *rig
				if enc {
					dir = encryptedClean(t)
					r = encRig(t, dir)
				} else {
					dir = plainBesidePersonsFiles(t)
					r = newRig(t, dir)
				}
				crashAt(t, c.at, dir, r.x.Log, enc)
				b, there := ma1046Marker(dir)
				if there != c.marker || (there && (len(b) == 0) != c.emptyMark) {
					t.Fatalf("after the crash: marker there %v (%d bytes), want %v (empty %v)", there, len(b), c.marker, c.emptyMark)
				}
				if n := len(ma1046Marked(t, r.x.Log)); n != c.marked {
					t.Fatalf("after the crash: %d marked lines, want %d: %q", n, c.marked, events(logLines(t, r.x.Log)))
				}
				if c.at6 != nil {
					c.at6(t, dir, r, enc)
				}
				if c.marker {
					before := ma1048Names(dir)
					err := ma1048Backup(dir, filepath.Join(t.TempDir(), LogName), ma1046Mbox(t))
					assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+dir+")"),
						assure.NoSideEffect(func() bool { return strings.Join(before, "\x00") == strings.Join(ma1048Names(dir), "\x00") }))
				}

				p := r.plan(t)
				if wantRerun := c.at == "anchor" || c.at == "marker"; (p.Rerun == RerunNoAnchor) != wantRerun {
					t.Fatalf("Delete again: rerun %v", p.Rerun)
				}
				res := Execute(p, p.Name, false, r.x)
				if exists(filepath.Join(dir, delmark.Name)) {
					t.Fatalf("a marker after Delete again:\n%s", res.Message())
				}
				if !enc && !exists(filepath.Join(dir, "taxes.pdf")) {
					t.Fatal("the person's file was removed")
				}
				if c.at == "marker" {
					if res.Status == StatusComplete || !res.Unfinished {
						t.Fatalf("no marker and no anchor: never complete (§5.4):\n%s", res.Message())
					}
					return
				}
				if res.Status != StatusComplete || len(r.rows) != 1 {
					t.Fatalf("Delete again: rows %v\n%s", r.rows, res.Message())
				}
				if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u != nil {
					t.Fatalf("the delete is finished in this computer's log: %+v", u)
				}
				if enc {
					if len(r.kr.forgot) != 1 || exists(dir) {
						t.Fatalf("the key forgotten (%v), the folder gone (%v):\n%s", r.kr.forgot, exists(dir), res.Message())
					}
					return
				}
				// Only the person's files are left: no folder the archive emptied (MED-1).
				for _, d := range []string{"Mailbox/Sent/Deep", "Mailbox/Sent"} {
					if exists(filepath.Join(dir, filepath.FromSlash(d))) {
						t.Errorf("the archive's emptied folder %s is left", d)
					}
				}
				// The person's files, as the plan names them: inside a store tree each file, at the
				// root each entry once; the no-anchor re-run names only root entries.
				want := "The folder is kept: it holds 3 files that are not the archive's: Mailbox/Inbox/notes.txt, Photos, taxes.pdf."
				if p.Rerun == RerunNoAnchor {
					want = "The folder is kept: it holds 3 files that are not the archive's: Mailbox, Photos, taxes.pdf."
				}
				if !strings.Contains(res.Lines[0], want) {
					t.Fatalf("Delete again's first line:\n%s\nwant it to contain %q", res.Lines[0], want)
				}
			})
		}
	}

	// MED-1's own case: an archive beside none of the person's files, killed right after its
	// manifest; Delete again removes the marker and the folder, and says so.
	t.Run("anchor, nothing of the person's", func(t *testing.T) {
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		crashAt(t, "anchor", dir, r.x.Log, false)
		if _, there := ma1046Marker(dir); !there || exists(filepath.Join(dir, manifestName)) {
			t.Fatal("after the crash: the marker there, the manifest gone")
		}
		if exists(filepath.Join(dir, "Mailbox")) {
			t.Fatal("after the crash: the archive's emptied folders went before the manifest")
		}
		res := r.run(t, false)
		if res.Status != StatusComplete || exists(dir) || !strings.Contains(res.Lines[0], "The folder is gone.") || strings.Contains(res.Message(), "not the archive's") {
			t.Fatalf("Delete again: the folder there %v:\n%s", exists(dir), res.Message())
		}
	})
}

// covers: MA-1047, R36
// The folders the archive emptied go before the anchor (the slice-3 pass's MED-1, a correction
// beside DM-9): every folder the plan lists is asked for after the files it held and before the
// late state files, the manifest and the marker; the removal right after the manifest is still the
// marker, and after it only the lock file. Positive twin first: the plan lists those folders, and
// the manifest is the last name the plan lists.
func TestMA1047_TheArchivesEmptiedFoldersGoBeforeTheAnchor(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	p := r.plan(t)
	if len(p.Folders) == 0 || p.Remove[len(p.Remove)-1].Rel != manifestName {
		t.Fatalf("positive twin: folders %q, last %q", p.Folders, p.Remove[len(p.Remove)-1].Rel)
	}
	var seen []string
	faultOn(t, &seen, nil)
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	index := func(rel string) int {
		for i, s := range seen {
			if s == rel {
				return i
			}
		}
		return -1
	}
	m, firstLate := index(manifestName), len(seen)
	for _, n := range lateStateFiles {
		if i := index(n); i >= 0 && i < firstLate {
			firstLate = i
		}
	}
	if m < 0 || firstLate > m {
		t.Fatalf("the manifest and the late state files: %q", seen)
	}
	for _, d := range p.Folders {
		if i := index(d); i < 0 || i > firstLate {
			t.Errorf("the folder %s was not asked for before the late state files and the manifest: %q", d, seen)
		}
	}
	if m+1 >= len(seen) || seen[m+1] != delmark.Name {
		t.Fatalf("the marker right after the manifest: %q", seen[m:])
	}
	for _, s := range seen[m+2:] {
		if s != lockName {
			t.Errorf("%s was removed after the marker: only the lock file may follow it", s)
		}
	}
	for _, d := range []string{"Mailbox/Sent/Deep", "Mailbox/Sent"} {
		if exists(filepath.Join(dir, filepath.FromSlash(d))) {
			t.Errorf("the emptied folder %s is left", d)
		}
	}
}

// covers: MA-1046, MA-1047, R36
// A hard link to the person's file planted at the marker's name after the plan (the slice-3 pass's
// probe, kept as a guard; DM-26): step 6 finds a regular file there and keeps it as it is — nothing
// is written through it — and removes that name last, which leaves the person's file whole. Positive
// twin first: the same delete with no link writes the delete's own marker.
func TestMA1047_AHardLinkPlantedAtTheMarkersNameIsNeverWrittenThrough(t *testing.T) {
	{
		twin := newRig(t, plainBesidePersonsFiles(t))
		var got []byte
		atFirstRemoval(t, func() { got, _ = ma1046Marker(twin.dir) })
		if res := twin.run(t, false); res.Status != StatusComplete || len(got) == 0 {
			t.Fatalf("positive twin: %q\n%s", got, res.Message())
		}
	}
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	person := filepath.Join(dir, "taxes.pdf")
	before, err := os.ReadFile(person)
	if err != nil {
		t.Fatal(err)
	}
	r.sched.onRemove = func(string) {
		if err := os.Link(person, filepath.Join(dir, delmark.Name)); err != nil {
			t.Fatal(err)
		}
	}
	res := r.run(t, false)
	after, err := os.ReadFile(person)
	if err != nil || string(after) != string(before) {
		t.Fatalf("the person's file changed or went: %v %q\n%s", err, after, res.Message())
	}
	if res.Status != StatusComplete || exists(filepath.Join(dir, delmark.Name)) || len(ma1046Marked(t, r.x.Log)) != 0 {
		t.Fatalf("the planted name removed last, no marked line:\n%s", res.Message())
	}
}

// covers: MA-1047, MA-1049, R36
// The marker swapped for a folder after the delete wrote it (the slice-3 pass's probe, kept as a
// guard): the delete never removes the folder at the name — "Not fully deleted", the row "Delete not
// finished" — another computer's backup is refused, nothing written, and Delete again's plan refuses
// it in MA-1049's words (remove it by hand). Positive twin first: the marker left as written goes
// last and the delete is complete.
func TestMA1047_AMarkerSwappedForAFolderIsNeverRemovedAndKeepsRefusing(t *testing.T) {
	if res := newRig(t, plainBesidePersonsFiles(t)).run(t, false); res.Status != StatusComplete {
		t.Fatalf("positive twin: %s", res.Message())
	}
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	m := filepath.Join(dir, delmark.Name)
	atFirstRemoval(t, func() {
		if err := os.Remove(m); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(m, 0o700); err != nil {
			t.Fatal(err)
		}
	})
	res := r.run(t, false)
	if res.Status != StatusPartial || !res.Unfinished || !strings.HasPrefix(res.Lines[0], "Not fully deleted. 1 file could not be removed: "+delmark.Name+".") {
		t.Fatalf("%s", res.Message())
	}
	if fi, err := os.Lstat(m); err != nil || !fi.IsDir() || !exists(filepath.Join(dir, "taxes.pdf")) {
		t.Fatalf("the folder at the name and the person's file are kept: %v", err)
	}
	before := ma1048Names(dir)
	err := ma1048Backup(dir, filepath.Join(t.TempDir(), LogName), ma1046Mbox(t))
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names("(Folder: "+dir+")"),
		assure.NoSideEffect(func() bool { return strings.Join(before, "\x00") == strings.Join(ma1048Names(dir), "\x00") }))
	_, perr := Plan(dir, r.x.Deps)
	if ref := AsRefusal(perr); ref == nil || ref.Case != CaseMarkerNotRegular || ref.Message != delmark.MsgPlanNotRegular {
		t.Fatalf("Delete again: %v", perr)
	}
}
