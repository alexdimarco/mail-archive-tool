package source

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-322, R25, R10, R4, S47
// An Evolution store is read under R25 like the others: a link out of the store where a
// folder would be (a dot-named entry of a Maildir++ root, a child of a cache's folders
// directory) is a terminal gap and its target is never touched, and a pipe standing where
// a folder would be is an info row. Neither is dropped without a trace (R25: every
// refused entry that might hold mail is recorded), and what they name is never read. The
// twin first: the same stores without them read every message and record nothing.
func TestEvolutionRecordsALinkOutOfTheStore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links and FIFOs need privileges on Windows")
	}
	const marker = "TOP-SECRET-CONTENT"
	for _, layout := range []string{"maildir++", "cache"} {
		t.Run(layout, func(t *testing.T) {
			parent := t.TempDir()
			store := filepath.Join(parent, "evo")
			var place, want string // where the entries go, and the messages the twin reads
			if layout == "maildir++" {
				writeLocal(t, filepath.Join(store, "..maildir++"), "")
				writeLocal(t, filepath.Join(store, "cur", "i"), localMail("i"))
				writeLocal(t, filepath.Join(store, ".Real", "cur", "r"), localMail("r"))
				place, want = store, "[: i Real: r]"
			} else {
				writeLocal(t, filepath.Join(store, "folders", "INBOX", "cur", "i"), localMail("i"))
				writeLocal(t, filepath.Join(store, "folders", "Real", "cur", "r"), localMail("r"))
				place, want = filepath.Join(store, "folders"), "[INBOX: i Real: r]"
			}
			twin := walkLocal(t, store, OpenOptions{})
			if fmt.Sprint(twin.subjects) != want || len(twin.faults) != 0 {
				t.Fatalf("the store without links: %v, %+v; want %s and no fault", twin.subjects, twin.faults, want)
			}

			writeLocal(t, filepath.Join(parent, "outsidedir", "cur", "m"), localMail(marker))
			dot := ""
			if layout == "maildir++" {
				dot = "."
			}
			if err := os.Symlink(filepath.Join(parent, "outsidedir"), filepath.Join(place, dot+"Linked")); err != nil {
				t.Fatal(err)
			}
			if err := mkfifo(filepath.Join(place, dot+"Pipe")); err != nil {
				t.Skip("no FIFOs here:", err)
			}
			got := walkLocal(t, store, OpenOptions{})
			for _, m := range got.msgs {
				if strings.Contains(m.Subject, marker) || strings.Contains(m.PlainBody, marker) {
					t.Errorf("the walk read what a link out of the store names: %q", m.Subject)
				}
			}
			if fmt.Sprint(got.subjects) != want {
				t.Errorf("delivered %v; want the same messages as the twin, %s", got.subjects, want)
			}
			rel := dot + "Linked"
			if layout == "cache" {
				rel = "folders/Linked"
			}
			link, ok := got.fault("symlink-outside-store")
			if !ok || link.Class != state.ClassTerminal || link.Locator != "file:"+rel {
				t.Errorf("link fault %+v, %v; want a terminal symlink-outside-store at file:%s (it was dropped without a trace)", link, ok, rel)
			}
			pipeRel := dot + "Pipe"
			if layout == "cache" {
				pipeRel = "folders/Pipe"
			}
			pipe, ok := got.fault("not-a-message-file")
			if !ok || pipe.Class != state.ClassInfo || pipe.Locator != "file:"+pipeRel {
				t.Errorf("pipe fault %+v, %v; want an info not-a-message-file at file:%s", pipe, ok, pipeRel)
			}
		})
	}
}

// covers: MA-322, R25, R10, S47
// Discovery does not wait on a pipe (FC2): a store whose entry named like a mailbox is a
// FIFO is seen as a store, or as none, within a guard, however the entries sort (the real
// mailbox here sorts AFTER the pipe, so discovery reaches the pipe before it can stop at
// the real one); Open on a pipe and auto-discovery's check of a pipe named like a data
// file end the same way. Discovery runs before the lock is taken, so a hang here is a hang
// of a run that has not begun.
func TestDiscoveryDoesNotWaitOnAPipeNamedLikeAMailbox(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs need privileges on Windows")
	}
	within := func(what string, f func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { defer close(done); f() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s waited on a pipe", what)
		}
	}
	dir := t.TempDir()
	withReal := filepath.Join(dir, "with-real")
	writeLocal(t, filepath.Join(withReal, "Zeta"), "From a@example.com Mon Mar  3 09:00:00 2025\n"+strings.ReplaceAll(localMail("z"), "\r\n", "\n")+"\n")
	if err := mkfifo(filepath.Join(withReal, "Inbox")); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	within("IsMailStoreDir over a pipe named Inbox", func() {
		if !IsMailStoreDir(withReal) {
			t.Error("the store with a real mailbox beside the pipe is not seen as a store")
		}
	})

	onlyPipe := filepath.Join(dir, "only-pipe")
	if err := os.MkdirAll(onlyPipe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mkfifo(filepath.Join(onlyPipe, "Inbox")); err != nil {
		t.Fatal(err)
	}
	within("IsMailStoreDir over a store that is only a pipe", func() {
		if IsMailStoreDir(onlyPipe) {
			t.Error("a directory holding nothing but a pipe is a mail store")
		}
	})

	pipe := filepath.Join(dir, "box")
	if err := mkfifo(pipe); err != nil {
		t.Fatal(err)
	}
	within("Open on a pipe", func() {
		if s, err := Open(pipe); err == nil {
			s.Close()
			t.Error("a pipe was opened as a mail source")
		}
	})
	dataPipe := filepath.Join(dir, "mail.pst")
	if err := mkfifo(dataPipe); err != nil {
		t.Fatal(err)
	}
	within("DataFileReadable on a pipe named like a data file", func() {
		if DataFileReadable(dataPipe) {
			t.Error("a pipe named mail.pst was taken for a readable mailbox")
		}
	})
}

// covers: MA-322, R25, R10, S47
// A link whose target would block if it were touched is refused before anything is
// resolved (FC3), by either of the two ways a target can leave the store: the absolute
// path (the case in localwalk_test.go) and a relative one that climbs out of the root
// after a lexical clean. The walk, which must not open or even resolve the pipe it names,
// ends under its guard with each link recorded.
func TestAnEscapingLinkToAPipeIsRefusedUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links and FIFOs need privileges on Windows")
	}
	parent := t.TempDir()
	pipe := filepath.Join(parent, "trap")
	if err := mkfifo(pipe); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	store := filepath.Join(parent, "store")
	writeLocal(t, filepath.Join(store, "cur", "a"), localMail("a"))
	if err := os.Symlink("../../trap", filepath.Join(store, "cur", "esc")); err != nil {
		t.Fatal(err)
	}
	got := walkLocal(t, store, OpenOptions{})
	if fmt.Sprint(got.subjects) != "[: a]" || got.count("symlink-outside-store") != 1 {
		t.Errorf("delivered %v, faults %+v; want the one message and the one refused link", got.subjects, got.faults)
	}
	if f, ok := got.fault("symlink-outside-store"); !ok || f.Class != state.ClassTerminal || f.Locator != "file:cur/esc" {
		t.Errorf("fault %+v, %v; want a terminal symlink-outside-store at file:cur/esc", f, ok)
	}
}

// covers: MA-321, R10, R15, R1, S46
// MA-321's EIO case: a maildir file whose read fails with an I/O error (a dying disk; no
// file on a healthy one does it, so the read is failed through the test seam) is the same
// gap as a permission failure, one fillable file-unreadable at maildir:<folder>/<unique>
// with no message, and its remedy is the disk's, not the permissions'. The neighbours and
// the next folder are read.
func TestMaildirFileEIOIsContained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("maildir names carry a colon")
	}
	store := filepath.Join(t.TempDir(), "store")
	for _, f := range []string{"Inbox/cur/a:2,S", "Inbox/cur/b:2,S", "Inbox/cur/c:2,S", "Work/cur/d:2,S"} {
		writeLocal(t, filepath.Join(store, filepath.FromSlash(f)), localMail(strings.SplitN(filepath.Base(f), ":", 2)[0]))
	}
	restore := InjectMaildirReadFaults(func(rel string) error {
		if rel == "Inbox/cur/b:2,S" {
			return &fs.PathError{Op: "read", Path: rel, Err: errors.New("input/output error")}
		}
		return nil
	})
	defer restore()
	got := walkLocal(t, store, OpenOptions{})
	if fmt.Sprint(got.subjects) != "[Inbox: a Inbox: c Work: d]" || got.err != nil {
		t.Errorf("delivered %v (err %v); want a, c and d: one failing read costs that file only", got.subjects, got.err)
	}
	f, ok := got.fault("file-unreadable")
	if !ok || len(got.faults) != 1 || f.Scope != state.GapScopeItem || f.Class != state.ClassFillable || f.Locator != "maildir:Inbox/b" {
		t.Fatalf("faults %+v; want exactly one fillable item file-unreadable at maildir:Inbox/b", got.faults)
	}
	if class := state.GapErrnoClass(f.Detail); class != state.ErrnoIO {
		t.Fatalf("the fault's detail %q reads as errno class %q; want the I/O class", f.Detail, class)
	}
	if r := state.GapRemedy(f.Reason, state.SourceLocal, state.GapErrnoClass(f.Detail)); !strings.Contains(r, "check the drive") || strings.Contains(r, "permissions") {
		t.Errorf("remedy %q; want the disk's (check the drive), not the permissions'", r)
	}
}

// covers: MA-322, R25, R10
// A folder is detected by the lstat type of its cur and new, which may be a link: a
// symlink, or on Windows a junction or mount point (Go reports it as irregular). A linked
// one still makes its folder a maildir, so the walk judges it and records it; it is never
// silently no maildir at all (§4.5, "detected but linked"). A plain file or a pipe is no
// maildir directory.
func TestMaildirDetectionSeesALinkedCurOrNew(t *testing.T) {
	for _, c := range []struct {
		name string
		mode fs.FileMode
		want bool
	}{
		{"directory", fs.ModeDir | 0o755, true},
		{"symlink", fs.ModeSymlink | 0o777, true},
		{"windows junction or mount point (irregular)", fs.ModeIrregular, true},
		{"regular file", 0o644, false},
		{"pipe", fs.ModeNamedPipe, false},
	} {
		if got := maildirEntryMode(c.mode); got != c.want {
			t.Errorf("%s: maildirEntryMode(%v) = %v, want %v", c.name, c.mode, got, c.want)
		}
	}
}
