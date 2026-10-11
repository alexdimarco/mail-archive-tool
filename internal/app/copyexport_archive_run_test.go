package app

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// listedKeys is every key search.db lists, through a read-only list open.
func listedKeys(t *testing.T, s *ArchiveSource) []string {
	t.Helper()
	h, err := s.OpenList()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var out []string
	for _, n := range s.Tree() {
		tok, folder, _ := s.Locate(n.ID)
		if err := h.EachInFolder(tok, folder, func(r index.ListRow) error { out = append(out, r.Key+"|"+r.Subject); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// covers: MA-436, R9, R20, R29, S52
// The archive is never written (design §4.3, §4.14, XC20): for the configured archive and for another
// archive folder, every archive file except search.db-wal and -shm is byte-identical after a preview
// and a run, search.db's rows are unchanged (read through a mode=ro open), no .mailarchive.lock is made
// when there was none, and another folder, which is also opened immutable, gains no file at all; a
// folder that is read-only media is read all the same; and a Microsoft 365 export needs no archive
// location at all.
func TestArchiveIsNeverWrittenByAnExport(t *testing.T) {
	for _, other := range []bool{false, true} {
		name := "the configured archive"
		if other {
			name = "another archive folder"
		}
		t.Run(name, func(t *testing.T) {
			a := buildCEArchive(t)
			os.Remove(filepath.Join(a.dir, lockfile.Name))
			e := newCEEnv(t)
			skip := []string{"search.db-wal", "search.db-shm"}
			if other {
				skip = nil // nothing at all, not even a sidecar, may appear beside an index opened immutable
			}
			before := treeDigest(t, a.dir, skip...)
			s0, err := OpenArchive(a.dir, ArchiveOptions{Other: other})
			if err != nil {
				t.Fatal(err)
			}
			rowsBefore := strings.Join(listedKeys(t, s0), ";")
			s0.Close()
			if got := treeDigest(t, a.dir, skip...); got != before {
				t.Fatal("listing the index changed the archive")
			}

			s, err := OpenArchive(a.dir, ArchiveOptions{Other: other})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			p := a.plan(e, s, allKeys(s)) // the preview
			if got := treeDigest(t, a.dir, skip...); got != before {
				t.Fatal("the preview changed the archive")
			}
			res := a.run(e, p, s)
			if res.Counts.Exported != 6 {
				t.Fatalf("the run: %+v", res.Counts)
			}
			if got := treeDigest(t, a.dir, skip...); got != before {
				t.Error("the run changed the archive: a file's bytes differ or a file was added")
			}
			if _, err := os.Lstat(filepath.Join(a.dir, lockfile.Name)); err == nil {
				t.Error("an archive export made .mailarchive.lock: it takes no lock")
			}
			s1, err := OpenArchive(a.dir, ArchiveOptions{Other: other})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(listedKeys(t, s1), ";"); got != rowsBefore {
				t.Errorf("search.db's rows changed:\n%s\nafter\n%s", rowsBefore, got)
			}
			s1.Close()
			if other {
				for _, side := range []string{"search.db-wal", "search.db-shm", "search.db-journal"} {
					if _, err := os.Lstat(filepath.Join(a.dir, side)); err == nil {
						t.Errorf("%s appeared beside an index opened for another archive folder", side)
					}
				}
			}
			if n := index.OpenListHandles(); n != 0 {
				t.Errorf("%d index handle(s) left open", n)
			}
		})
	}

	// read-only media: nothing can be written there, and the export reads it all the same
	t.Run("read-only media", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("a read-only folder is modelled with permission bits, for a user who is not root")
		}
		a := buildCEArchive(t)
		os.Remove(filepath.Join(a.dir, lockfile.Name))
		var dirs []string
		filepath.WalkDir(a.dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				dirs = append(dirs, p)
			} else if err == nil {
				os.Chmod(p, 0o444)
			}
			return nil
		})
		for i := len(dirs) - 1; i >= 0; i-- {
			os.Chmod(dirs[i], 0o555)
		}
		t.Cleanup(func() {
			for _, d := range dirs {
				os.Chmod(d, 0o755)
			}
		})
		before := treeDigest(t, a.dir)
		e := newCEEnv(t)
		s, err := OpenArchive(a.dir, ArchiveOptions{Other: true})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if res := a.run(e, a.plan(e, s, allKeys(s)), s); res.Counts.Exported != 6 {
			t.Fatalf("an export from read-only media: %+v", res.Counts)
		}
		if got := treeDigest(t, a.dir); got != before {
			t.Error("the archive on read-only media changed")
		}
	})

	// a Microsoft 365 export works with no archive location set at all
	e, src := newCEEnv(t), newFakeSrc()
	p := cePlan(t, e, src, ceMsgs(3))
	if res := mustRun(t, e, p, src); res.Counts.Exported != 3 || res.Status != CopyStatusComplete {
		t.Errorf("a Microsoft 365 export with no archive location: %+v", res)
	}
}

// covers: MA-510, R29, R5, R20, S25, S52
// Concurrency, the lock-free archive half (design §4.14): an archive export takes no archive lock, so a
// capture can hold it throughout (and the export never backs off for one); a capture started in the
// middle of an export succeeds and writes its last-run record; a record a full capture re-exported
// in the middle is "changed in the archive during export" and never emitted mixed: every file of
// the export is the bytes the plan recorded.
func TestArchiveExportTakesNoLockBesideACapture(t *testing.T) {
	// a capture holds the archive lock for the whole export
	a := buildCEArchive(t)
	e := newCEEnv(t)
	e.SetCaptureBusy(func() bool { return true }) // a Microsoft 365 export would wait here for ever
	s := a.open()
	lk, err := lockfile.AcquireAs(filepath.Join(a.dir, lockfile.Name), "graph")
	if err != nil {
		t.Fatal(err)
	}
	before := treeDigest(t, a.dir)
	done := make(chan *CopyResult, 1)
	p := a.plan(e, s, allKeys(s))
	go func() { done <- a.run(e, p, s) }()
	select {
	case res := <-done:
		if res.Counts.Exported != 6 || res.Status != CopyStatusComplete {
			t.Errorf("the export beside a capture that holds the lock: %+v", res)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the archive export waited for the capture that holds the lock")
	}
	if got := treeDigest(t, a.dir); got != before {
		t.Error("the export changed the archive beside a capture")
	}
	lk.Release()

	// a capture starts in the middle of an export and succeeds; and a full capture re-exports a record
	a = buildCEArchive(t)
	e = newCEEnv(t)
	s = a.open()
	keys := []string{a.key("hello"), a.key("second"), a.key("client-mail")}
	p = a.plan(e, s, keys)
	alice := filepath.Join(a.src, "alice")
	var capErr error
	captured := false
	e.faults = &copyFaults{hook: func(step string) {
		if step != "W6:1" || captured {
			return
		}
		captured = true
		writeMaildirMsg(t, alice, "a3.eml", msg("a message from the middle", "id9", "body nine"))
		// a re-captured message: its source changed, so a full capture rewrites its eml and page
		writeMaildirMsg(t, alice, "a2.eml", msg("second", "id2", "body two, as the source has it now"))
		opts := Options{Inputs: []string{alice}, Out: a.dir, Mode: export.Full, Index: true, Pages: true, KeepRaw: true}
		_, capErr = Run(context.Background(), opts, discard(), nil)
	}}
	res := a.run(e, p, s)
	if !captured || capErr != nil {
		t.Fatalf("the capture started in the middle of the export: ran %v, error %v", captured, capErr)
	}
	lr, st, lerr := state.ReadLastRun(a.dir)
	if lerr != nil || st != state.LastRunPresent || lr.Status != state.RunOK {
		t.Errorf("the capture's last-run record: %+v (%v, %v)", lr, st, lerr)
	}
	man := manifestOf(t, res.Root)
	second := a.key("second")
	f := failureListed(man, second)
	if f == nil || f["class"] != "retryable" || !strings.Contains(f["reason"].(string), "changed in the archive during export") {
		t.Fatalf("the record the capture rewrote: %v, want it listed as changed in the archive during export (retryable)", f)
	}
	for _, m := range p.Messages {
		ent := entryOf(t, man, m.SourceID)
		for _, fl := range ent["files"].([]any) {
			fl := fl.(map[string]any)
			want := m.Fix.EML
			if fl["role"] != "original" || want == nil || fl["sha256"] != want.SHA256 {
				t.Errorf("%v: an exported file does not match the fixity the plan recorded: %v", m.Subject, fl)
			}
		}
	}
	if entryOf(t, man, a.key("hello"))["status"] != "exported" {
		t.Errorf("the message exported before the capture: %v", entryOf(t, man, a.key("hello")))
	}
	if sums, missing, extra := sumsCheck(t, res.Root); len(sums)+len(missing)+len(extra) != 0 {
		t.Errorf("the partial export does not verify: %v %v %v", sums, missing, extra)
	}
}

// covers: MA-427, MA-512, R29, R5, R12
// The archive's root or manifest gone after a read error is run-fatal R-o2 ("the archive is no longer
// available"), not a list of thousands of false "damaged" messages; "Export the remaining messages" is
// offered, its part holds the messages not attempted and the one in flight, and once the archive is
// back it exports them (design §4.6 item 7, XC10).
func TestCopyExportArchiveGoneMidRunIsRO2(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	s := a.open()
	keys := allKeys(s)
	p := a.plan(e, s, keys)
	stash := filepath.Join(tmpDir(t), "stash")
	os.MkdirAll(stash, 0o755)
	var moved []string
	e.faults = &copyFaults{hook: func(step string) {
		if step != "W6:2" || moved != nil {
			return
		}
		ents, _ := os.ReadDir(a.dir) // the share is disconnected: everything in it is out of reach
		for _, ent := range ents {
			if err := os.Rename(filepath.Join(a.dir, ent.Name()), filepath.Join(stash, ent.Name())); err != nil {
				t.Errorf("disconnect: %v", err)
			}
			moved = append(moved, ent.Name())
		}
	}}
	res := a.run(e, p, s)
	if moved == nil {
		t.Fatal("the archive was never disconnected")
	}
	if res.Stop == nil || res.Stop.RID != "R-o2" || res.Stop.Class != stopClassArchive || res.Status != CopyStatusFailed || res.Outcome != CopyOutcomeFailed {
		t.Fatalf("the run: status %s, outcome %d, stop %+v; want the run-fatal R-o2", res.Status, res.Outcome, res.Stop)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names(a.dir, "no longer available", "nothing in the archive is damaged", "Export the remaining messages", "of 6 messages"))
	if res.Counts.Exported != 2 || res.Counts.Failed != 1 || res.Counts.NotAttempted != 3 {
		t.Errorf("counts %+v: want 2 exported, the message in flight listed (retryable, with the stop's reason) and 3 never tried, not 4 false 'damaged' failures", res.Counts)
	}
	man := manifestOf(t, res.Root)
	if f := failureListed(man, p.Messages[2].SourceID); f == nil || f["class"] != "retryable" || !strings.Contains(f["reason"].(string), "R-o2") {
		t.Errorf("the message in flight: %v", f)
	}
	if man["run"].(map[string]any)["status"] != "failed" || !exists(res.Root, copyMarkerName) {
		t.Errorf("the stopped run is finalized failed with its marker kept: %v", man["run"])
	}
	if got := readText(t, res.Root, copyMarkerName); !strings.Contains(got, "STOPPED") {
		t.Errorf("the marker: %q", got)
	}

	// Export the remaining messages: it needs the run record, not the archive
	in, err := e.RetryInput(res.RunID, "rest", "")
	if err != nil {
		t.Fatalf("Export the remaining messages: %v", err)
	}
	if len(in.Messages) != 4 || in.Messages[3].SourceID != p.Messages[2].SourceID || in.Source.Archive != a.dir {
		t.Fatalf("the remaining messages: %d of them, in-flight last: %v", len(in.Messages), len(in.Messages))
	}
	for _, name := range moved { // reconnected
		if err := os.Rename(filepath.Join(stash, name), filepath.Join(a.dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	s2 := a.open()
	var rest []string
	for _, m := range in.Messages {
		rest = append(rest, m.SourceID)
	}
	rp := a.plan(e, s2, rest)
	rp.Chain = in.Chain
	rp2, err := e.PlanCopyExport(CopyPlanInput{Source: s2.Describe(), Messages: rp.Messages, Folders: foldersOf(rp.Messages), Dest: rp.Dest, Options: in.Options, Chain: in.Chain, Archives: []string{a.dir}})
	if err != nil {
		t.Fatal(err)
	}
	part2 := a.run(e, rp2, s2)
	if part2.Counts.Exported != 4 || part2.Status != CopyStatusComplete {
		t.Errorf("the second part: %+v", part2.Counts)
	}
}
