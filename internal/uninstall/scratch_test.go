package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/schedule"
)

// noSystem stubs the schedule sweep and the sign-in clear, so a test touches no real
// OS scheduler or credential store.
func noSystem(t *testing.T) {
	t.Helper()
	origList, origRemove, origClear := listSchedules, removeSchedule, clearSignIn
	t.Cleanup(func() { listSchedules, removeSchedule, clearSignIn = origList, origRemove, origClear })
	listSchedules = func() []schedule.InstalledEntry { return nil }
	removeSchedule = func(schedule.Spec) error { return nil }
	clearSignIn = func(string, string) error { return nil }
}

// scratchFixture fills the package's scratch folder (TestMain points it at a throwaway
// one) with what an encrypted archive's run leaves — one archive's snapshot and
// Outlook PST — and returns the folder and the two files.
func scratchFixture(t *testing.T) (root string, files []string) {
	t.Helper()
	root, err := scratchRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	arch := filepath.Join(root, "0123456789abcdef0123456789abcdef")
	for _, d := range []string{"snap", "_outlook-pst"} {
		if err := os.MkdirAll(filepath.Join(arch, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files = []string{filepath.Join(arch, "snap", snapshotPrefix+"123.ost"), filepath.Join(arch, "_outlook-pst", "Mailbox.PST")}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("plaintext mail"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root, files
}

func listTree(root string) []string {
	var all []string
	filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err == nil {
			all = append(all, p)
		}
		return nil
	})
	sort.Strings(all)
	return all
}

// covers: MA-739, R36, R37
// Uninstall removes MailArchive's scratch folder — an encrypted archive's leftover
// working copies — and ONLY what it owns there: exactly the snapshots and PSTs of the
// shape a run writes, then each folder only when empty, the root last; a dry run lists
// them and removes nothing; the settings files are still removed. Anything else — a
// stray file, a file of another name, a folder of another name, a symlink — or a
// scratch folder that overlaps a known archive refuses the WHOLE uninstall before
// anything is removed (fail-closed, R36); an absent scratch folder is nothing to do.
func TestUninstallRemovesOnlyTheScratchItOwns(t *testing.T) {
	noSystem(t)
	if snapshotPrefix != export.SnapshotPrefix {
		t.Fatalf("uninstall's snapshot prefix %q is not the exporter's %q", snapshotPrefix, export.SnapshotPrefix)
	}
	if got, err := ScratchTargets(filepath.Join(t.TempDir(), "absent"), nil); err != nil || got != nil {
		t.Fatalf("an absent scratch folder: %v %v", got, err)
	}

	// The positive twin: a well-formed scratch folder is listed, then removed whole.
	root, files := scratchFixture(t)
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, "last-archive.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Uninstall(cfg, nil, "", true)
	listedFiles := []string{}
	if err == nil && len(rep.Scratch) == 6 {
		listedFiles = append(listedFiles, rep.Scratch[:2]...)
		sort.Strings(listedFiles)
		sort.Strings(files)
	}
	if err != nil || len(rep.Scratch) != 6 || !reflect.DeepEqual(listedFiles, files) || rep.Scratch[5] != root {
		t.Fatalf("the dry run must list the owned files, then their folders, then the root: %+v (%v)", rep, err)
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Fatal("a dry run removed a scratch file")
	}
	if _, err := Uninstall(cfg, nil, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("the scratch folder must be gone after an uninstall (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "last-archive.txt")); !os.IsNotExist(err) {
		t.Fatal("the settings files must still be removed")
	}

	for label, plant := range map[string]func(t *testing.T, root string){
		"a stray file at the root": func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o600)
		},
		"a file of another name in snap": func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "0123456789abcdef0123456789abcdef", "snap", "thesis.docx"), nil, 0o600)
		},
		"a folder of another name": func(t *testing.T, root string) {
			os.Mkdir(filepath.Join(root, "0123456789abcdef0123456789abcdef", "photos"), 0o700)
		},
		"a symlink to a real folder": func(t *testing.T, root string) {
			if err := os.Symlink(t.TempDir(), filepath.Join(root, "fedcba9876543210fedcba9876543210")); err != nil {
				t.Skip("no symlinks here:", err)
			}
		},
	} {
		t.Run(label, func(t *testing.T) {
			root, _ := scratchFixture(t)
			plant(t, root)
			cfg := t.TempDir()
			if err := os.WriteFile(filepath.Join(cfg, "last-archive.txt"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := listTree(root)
			if _, err := Uninstall(cfg, nil, "", false); !errors.Is(err, ErrScratchUnexpected) {
				t.Fatalf("want ErrScratchUnexpected, got %v", err)
			}
			if !reflect.DeepEqual(before, listTree(root)) {
				t.Fatal("a refused uninstall removed something from the scratch folder")
			}
			if _, err := os.Stat(filepath.Join(cfg, "last-archive.txt")); err != nil {
				t.Fatal("a refused uninstall removed a settings file")
			}
		})
	}

	t.Run("overlapping a known archive", func(t *testing.T) {
		root, _ := scratchFixture(t)
		before := listTree(root)
		if _, err := Uninstall(t.TempDir(), []string{filepath.Dir(root)}, "", false); !errors.Is(err, ErrOverlap) {
			t.Fatalf("want ErrOverlap, got %v", err)
		}
		if !reflect.DeepEqual(before, listTree(root)) {
			t.Fatal("a refused uninstall removed something from the scratch folder")
		}
	})
}
