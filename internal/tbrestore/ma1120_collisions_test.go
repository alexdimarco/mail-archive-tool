package tbrestore

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1120, R40
// Collisions (design §3.5 as folded): an existing "Restored — NAME", its ".sbd" alone,
// or a case variant gives "Restored — NAME (2)"; all 99 taken is W-NAMES; nothing that
// was there is changed (the listing and every file's hash before and after); a name
// taken between the plan and the Mkdir (by anything) gives the next " (n)", a fsynced
// "name" line naming it, and the restore runs to the end under it.
func TestRestoreCollisions(t *testing.T) {
	in, msgs := sampleFolders()
	for name, existing := range map[string][]string{
		"file":           {"Restored — Work mail"},
		"sbd only":       {"Restored — Work mail.sbd/"},
		"case variant":   {"restored — WORK MAIL"},
		"(2) taken also": {"Restored — Work mail", "Restored — Work mail (2).sbd/"},
	} {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t, in, msgs)
			for _, e := range existing {
				if e[len(e)-1] == '/' {
					if err := os.Mkdir(filepath.Join(fx.lf.Dir, e[:len(e)-1]), 0o700); err != nil {
						t.Fatal(err)
					}
				} else {
					writeFile(t, filepath.Join(fx.lf.Dir, e), "the person's own")
				}
			}
			fx.top = fx.freeTop()
			want := "Restored — Work mail (2)"
			if name == "(2) taken also" {
				want = "Restored — Work mail (3)"
			}
			if fx.top != want {
				t.Fatalf("free name: %q, want %q", fx.top, want)
			}
			before := snapshot(t, fx.lf.Dir)
			res, _ := fx.write(context.Background(), nil)
			if res.Outcome != OutDone || res.Top != want {
				t.Fatalf("%s %v %q", res.Outcome, res.Err, res.Top)
			}
			// Everything that was there is unchanged; only the new pair is added.
			after := snapshot(t, filepath.Join(fx.lf.Dir), want, want+".sbd")
			if after != before {
				t.Fatalf("something that was there changed:\n%s\n---\n%s", before, after)
			}
		})
	}

	t.Run("99 taken", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		for n := 1; n <= 99; n++ {
			writeFile(t, filepath.Join(fx.lf.Dir, withNumber(fx.layout.Top, n)), "")
		}
		before := snapshot(t, fx.lf.Dir)
		entries, _ := os.ReadDir(fx.lf.Dir)
		_, _, err := FreeTopName(fx.layout.Top, entries)
		refusedAs(t, err, "W-NAMES", func() bool { return snapshot(t, fx.lf.Dir) == before }, "'Restored — Work mail' and 98 more", "Delete or rename")
	})

	t.Run("taken between the plan and the Mkdir", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
			if op == "mkdir" && name == "Restored — Work mail.sbd" {
				_ = os.Mkdir(filepath.Join(fx.lf.Dir, name), 0o700) // someone else's, made in that instant
			}
			return nil
		}}
		res, id := fx.write(context.Background(), f)
		if res.Outcome != OutDone || res.Top != "Restored — Work mail (2)" {
			t.Fatalf("%s %v %q", res.Outcome, res.Err, res.Top)
		}
		if ev := fx.events(id); dump(ev) != "[started name checked ended]" {
			t.Fatalf("a fsynced name line before going on: %v", ev)
		}
		if es, _ := os.ReadDir(filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd")); len(es) != 0 {
			t.Fatal("the other folder of that name was written into")
		}
	})
}

// covers: MA-1120, R40
// os.Root.Mkdir and an O_EXCL create are exclusive (lab C showed .NET's
// CreateDirectory is not, so this is proven, not assumed): each fails, with an error
// satisfying fs.ErrExist, on an existing file, an existing folder, and — on a volume
// that ignores case (NTFS, APFS by default; the Windows leg runs on the VM) — a case
// variant; nothing that was there changes.
func TestRestoreExclusiveCreates(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f := newRootFS(root)
	writeFile(t, filepath.Join(dir, "afile"), "kept")
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o700); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)
	for _, n := range []string{"afile", "adir"} {
		if err := f.Mkdir(n); !errors.Is(err, fs.ErrExist) {
			t.Errorf("Mkdir on an existing %s: %v", n, err)
		}
		if w, err := f.Create(n); err == nil { // refused: exists, or (Windows, a folder) is a directory
			if w != nil {
				w.Close()
			}
			t.Errorf("Create on an existing %s: %v", n, err)
		}
	}
	caseBlind := false
	if _, err := os.Stat(filepath.Join(dir, "AFILE")); err == nil {
		caseBlind = true
	}
	if caseBlind {
		if err := f.Mkdir("AFILE"); !errors.Is(err, fs.ErrExist) {
			t.Errorf("Mkdir on a case variant: %v", err)
		}
		if _, err := f.Create("ADIR"); err == nil {
			t.Error("Create on a case variant of a folder succeeded")
		}
		if _, err := f.Create("AFILE"); !errors.Is(err, fs.ErrExist) {
			t.Errorf("Create on a case variant: %v", err)
		}
	} else {
		t.Log("this volume keeps case apart: the case-variant leg is the Windows VM's and macOS's")
	}
	if snapshot(t, dir) != before {
		t.Fatal("a refused create changed what was there")
	}
}
