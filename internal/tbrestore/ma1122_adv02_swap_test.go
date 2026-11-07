package tbrestore

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The person's own folder file in Local Folders, named like a file the leftover lists.
const personMail = "From - x\nSubject: the person's own mail\n\nkeep me\n"

// abLeftover is a restore of A (with B below it) that crashed just before its commit:
// checked, no parent file — the state a later Start cleans up.
func abLeftover(t *testing.T) *fixture {
	t.Helper()
	in := []FolderIn{{Names: []string{"A"}, Messages: 1}, {Names: []string{"A", "B"}, Messages: 1}}
	msgs := [][][]byte{{[]byte("Subject: a\n\nx\n")}, {[]byte("Subject: b\n\ny\n")}}
	k := stepOf(t, in, msgs, "create "+TopName("Work mail"))
	fx := newFixture(t, in, msgs)
	if _, crashed := fx.writeCrashing(k); !crashed {
		t.Fatal("no crash")
	}
	return fx
}

// swapFS calls swap just before the cleanup's removal of one entry, or just before
// the Lstat the removal loop makes of it.
type swapFS struct {
	FS
	reads       int
	beforeLstat func(name string)
	beforeRm    func(name string)
}

// ReadDir counts the folder reads that have finished (the scan's two come first).
func (s *swapFS) ReadDir(n string, fn func([]fs.DirEntry) bool) error {
	err := s.FS.ReadDir(n, fn)
	s.reads++
	return err
}

func (s *swapFS) Lstat(n string) (fs.FileInfo, error) {
	if s.beforeLstat != nil {
		s.beforeLstat(n)
	}
	return s.FS.Lstat(n)
}

func (s *swapFS) RemoveSeen(n string, seen map[string]fs.FileInfo) error {
	if s.beforeRm != nil {
		s.beforeRm(n)
	}
	return s.FS.RemoveSeen(n, seen)
}

// covers: MA-1122, R40, S68
// ADV-02 (the R3–R5 adversarial pass, settled 2026-11-07): a cleanup removes an entry
// only when it, and every folder on the way to it, is still exactly the entry its scan
// saw — same identity, same kind, never a link, junction or reparse point — walking down
// from Local Folders one folder at a time by handle, following no link, and removing the
// entry relative to its verified parent. So a listed folder (A.sbd) swapped for a link
// to Local Folders between the scan and the removal of A.sbd/B cannot walk the removal
// into Local Folders: the person's own Local Folders/B, of the listed name, is kept byte
// for byte, the cleanup stops (W-LEFT's step-4 form) and the log says "abandoned". The
// swap is made at three instants: at the removal loop's Lstat (the adversarial pass's
// probe P2), at the call of the removal itself, and inside it between its check and the
// unlink (where the entry itself is swapped for a link to the person's file: the link is
// never followed). Positive twin: with no swap the leftover is cleaned.
func TestRestoreCleanupNeverFollowsASwappedLink(t *testing.T) {
	type when string
	const (
		atLstat  when = "at the removal loop's Lstat"
		atRemove when = "at the removal's call"
		inside   when = "inside the removal, after its check"
	)
	for _, kind := range linkKinds() {
		for _, w := range []when{atLstat, atRemove, inside} {
			t.Run(kind+" "+string(w), func(t *testing.T) {
				fx := abLeftover(t)
				user := filepath.Join(fx.lf.Dir, "B")
				writeFile(t, user, personMail)
				container := fx.top + ".sbd"
				target := container + "/A.sbd/B"
				swapped, linkErr := false, error(nil)
				swapDir := func() {
					real := filepath.Join(fx.lf.Dir, container, "A.sbd")
					if linkErr = os.Rename(real, real+"-moved"); linkErr == nil {
						linkErr = makeLink(kind, real, fx.lf.Dir, "..")
					}
					swapped = true
				}
				f := &swapFS{FS: newRootFS(fx.root)}
				switch w {
				case atLstat:
					f.beforeLstat = func(name string) {
						if !swapped && f.reads >= 2 && name == target {
							swapDir()
						}
					}
				case atRemove:
					f.beforeRm = func(name string) {
						if !swapped && name == target {
							swapDir()
						}
					}
				case inside:
					if kind != "symlink" {
						t.Skip("a junction cannot stand for a file")
					}
					was := beforeUnlinkSeam
					beforeUnlinkSeam = func(name string) {
						if swapped || name != target {
							return
						}
						swapped = true
						p := filepath.Join(fx.lf.Dir, filepath.FromSlash(target))
						if linkErr = os.Rename(p, p+"-moved"); linkErr == nil {
							linkErr = makeLink(kind, p, user, filepath.Join("..", "..", "B"))
						}
					}
					t.Cleanup(func() { beforeUnlinkSeam = was })
				}
				s, err := Settle(SettleInput{Log: fx.log, Local: fx.lf, FS: f, ProfileDir: fx.profile})
				if linkErr != nil {
					if kind == "symlink" && isNoSymlinkPrivilege(linkErr) {
						t.Skipf("this user cannot make symbolic links: %v", linkErr)
					}
					t.Fatalf("the swap: %v", linkErr)
				}
				if !swapped || err != nil || len(s) != 1 {
					t.Fatalf("the swap was made and the leftover settled: swapped %v, %+v %v", swapped, s, err)
				}
				rc, msg := 0, ""
				if s[0].Action == EvAbandoned && s[0].Clean != nil && s[0].Clean.Kind == CleanLeftPart {
					rc, msg = 1, s[0].Words
				}
				refusedAs(t, errFrom(rc, msg), "W-KEPT", func() bool {
					b, err := os.ReadFile(user)
					return err == nil && string(b) == personMail
				}, "removed part of what it wrote", "found something there it did not make", fx.top)
			})
		}
	}
	t.Run("positive twin: no swap, cleaned", func(t *testing.T) {
		fx := abLeftover(t)
		user := filepath.Join(fx.lf.Dir, "B")
		writeFile(t, user, personMail)
		s, err := Settle(SettleInput{Log: fx.log, Local: fx.lf, FS: &swapFS{FS: newRootFS(fx.root)}, ProfileDir: fx.profile})
		if err != nil || len(s) != 1 || s[0].Action != EvCleaned || fileExists(filepath.Join(fx.lf.Dir, fx.top+".sbd")) || !fileExists(user) {
			t.Fatalf("cleaned, the person's B kept: %+v %v", s, err)
		}
	})
}
