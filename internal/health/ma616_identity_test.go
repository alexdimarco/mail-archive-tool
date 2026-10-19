package health

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-616, R33, R18, S59
// The composed identity predicate (§4.6.2), first match wins, over the
// composition trace: missing; a translocated copy; the same file; a same-dir
// known-name sibling that qualifies (program_name), is a different build (skew)
// or another account (exe_moved); a renamed running file that never vouches; a
// former name in another directory (exe_moved, never the size/mtime fallback,
// even when size and mtime match); a same-build copy in another directory with no
// recorded mtime (exe_moved, never "this program"); and the size/mtime fallback
// (unchanged_since_install). schedule.SiblingQualifies itself is proven by MA-615;
// here it is a seam so each rule-4 reason is exercised directly.
func TestMA616_ComposedIdentity(t *testing.T) {
	origSelf, origSib := schedule.SelfResolver, siblingQualifies
	t.Cleanup(func() { schedule.SelfResolver, siblingQualifies = origSelf, origSib })

	tmp := t.TempDir()   // the program's own directory
	other := t.TempDir() // a different directory
	write := func(dir, name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("MZ-fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	statOf := func(p string) (int64, func() schedule.Descriptor) {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return fi.Size(), func() schedule.Descriptor {
			return schedule.Descriptor{Exe: p, ExeSize: fi.Size(), ExeMTime: fi.ModTime().UTC()}
		}
	}

	// Files used by the cases.
	recW := write(tmp, "mailarchivew.exe") // the sibling in the program's own dir
	transDir := filepath.Join(tmp, "AppTranslocation", "d")
	if err := os.MkdirAll(transDir, 0o755); err != nil {
		t.Fatal(err)
	}
	recTrans := write(transDir, "mailarchive")
	recFormer := write(other, "mailarchive-gui")            // a former name, elsewhere
	recAsset := write(other, "mailarchive-gui-linux-amd64") // a former release asset, elsewhere
	recElsewhere := write(other, "mailarchivew.exe")        // a known name, but in another dir
	recArb := write(other, "weekly-backup-runner")          // not a program name
	_, formerDesc := statOf(recFormer)
	_, arbDesc := statOf(recArb)

	selfIn := filepath.Join(tmp, "mailarchive")           // self in the program's own dir (need not exist)
	selfRenamed := filepath.Join(tmp, "MailArchive-copy") // a renamed running file (not a known name)
	selfElse := filepath.Join(other+"-self", "mailarchive")

	qual := func(ok bool, r schedule.Reason) func(string, string) (bool, schedule.Reason) {
		return func(self, p string) (bool, schedule.Reason) { return ok, r }
	}

	cases := []struct {
		name       string
		self       string
		desc       schedule.Descriptor
		sib        func(string, string) (bool, schedule.Reason)
		wantExists bool
		wantIsThis bool
		wantBasis  string
		wantSkew   bool
	}{
		{"missing", selfIn, schedule.Descriptor{Exe: filepath.Join(tmp, "gone")}, nil, false, false, "", false},
		{"translocated copy", selfIn, schedule.Descriptor{Exe: recTrans}, nil, true, false, "", false},
		{"same file", recW, schedule.Descriptor{Exe: recW}, nil, true, true, "same_file", false},
		{"program_name sibling qualifies", selfIn, schedule.Descriptor{Exe: recW}, qual(true, schedule.ReasonSibling), true, true, "program_name", false},
		{"a different build is skew", selfIn, schedule.Descriptor{Exe: recW}, qual(false, schedule.ReasonPairOutOfStep), true, false, "", true},
		{"another account is exe_moved not skew", selfIn, schedule.Descriptor{Exe: recW}, qual(false, schedule.ReasonSiblingOtherOwner), true, false, "", false},
		{"a renamed running file never vouches", selfRenamed, schedule.Descriptor{Exe: recW}, qual(true, schedule.ReasonSibling), true, false, "", false},
		{"former name elsewhere, matching size/mtime, never the fallback", selfElse, formerDesc(), nil, true, false, "", false},
		{"former release asset elsewhere", selfElse, schedule.Descriptor{Exe: recAsset}, nil, true, false, "", false},
		{"same-name copy in another dir, no recorded mtime", selfElse, schedule.Descriptor{Exe: recElsewhere}, nil, true, false, "", false},
		{"size/mtime fallback is unchanged_since_install", selfElse, arbDesc(), nil, true, true, "unchanged_since_install", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			schedule.SelfResolver = func() (string, error) { return c.self, nil }
			siblingQualifies = origSib
			if c.sib != nil {
				siblingQualifies = c.sib
			}
			id := exeIdentity(c.desc)
			if id.exists != c.wantExists || id.isThis != c.wantIsThis || id.basis != c.wantBasis || id.skew != c.wantSkew {
				t.Fatalf("got %+v, want exists=%v isThis=%v basis=%q skew=%v",
					id, c.wantExists, c.wantIsThis, c.wantBasis, c.wantSkew)
			}
		})
	}
}
