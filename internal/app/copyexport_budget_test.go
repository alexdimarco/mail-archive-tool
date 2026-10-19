package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
)

// longSubject is a subject whose slug is the full 60 runes.
const longSubject = "a quarterly review of the regional budget and the forecast for next year"

// slugOf is the subject-slug part of a stem "<ts>_<slug>_<8 hex>" ("" when the slug
// was dropped: "<ts>_<8 hex>").
func slugOf(stem string) string {
	parts := strings.Split(stem, "_")
	if len(parts) <= 3 {
		return ""
	}
	return strings.Join(parts[2:len(parts)-1], "_")
}

// covers: MA-441, R4, R29
// Path budget: as the destination's own path grows, the subject slug shrinks 60 → 24 →
// 0 before any path is reported over budget (an absolute path stays within 250 UTF-16
// units while a slug can still shrink); the plan reports the longest path in UTF-16
// units and the count over 259; and folder segments are never truncated beyond the
// naming pass's own rule, whatever the path budget.
func TestCopyExportPathBudget(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	msgs := ceMsgs(3)
	for i := range msgs {
		msgs[i].Subject = longSubject
	}
	dest := filepath.Join(t.TempDir(), "dest")
	seen := map[int]bool{}
	last := 61
	for depth := 0; depth < 60; depth++ {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		p := cePlanAt(t, e, src, msgs, dest)
		first := p.SamplePaths[0]
		stem := strings.TrimSuffix(filepath.Base(first), ".eml")
		slug := len([]rune(slugOf(stem)))
		abs := filepath.ToSlash(filepath.Join(dest, p.ExportName)) + "/" + first
		n := utf16Len(abs)
		seen[slug] = true
		if slug > last {
			t.Fatalf("the slug grew from %d to %d as the destination got longer", last, slug)
		}
		last = slug
		switch {
		case slug == 60 && n > copyPathBudget:
			t.Fatalf("slug 60 kept at %d units, over the budget of %d", n, copyPathBudget)
		case slug == 24 && n > copyPathBudget:
			t.Fatalf("slug 24 kept at %d units, over the budget of %d", n, copyPathBudget)
		}
		if slug == 0 {
			// Both slugs were tried first: 24 would have been over the budget.
			if with24 := n + 24 + 1; with24 <= copyPathBudget {
				t.Fatalf("the slug was dropped at %d units though 24 runes would fit (%d)", n, with24)
			}
			if p.LongestPath < n || (n > copyPathWarn && p.Over259 != len(msgs)) || (n <= copyPathWarn && p.Over259 != 0) {
				t.Fatalf("the plan reports longest %d and %d over 259 for paths of %d units", p.LongestPath, p.Over259, n)
			}
			if n > copyPathWarn {
				break
			}
		}
		dest = filepath.Join(dest, strings.Repeat("d", 20))
	}
	for _, want := range []int{60, 24, 0} {
		if !seen[want] {
			t.Errorf("the slug never reached %d (saw %v): the shrink steps are 60, 24, 0", want, seen)
		}
	}
	// Folder segments are never truncated by the path budget: a 100-character folder
	// name in a destination that already puts every path over 259 keeps all 100.
	deep := ceMsgs(2, strings.Repeat("A", 100))
	if p := cePlanAt(t, e, src, deep, dest); !strings.Contains(p.SamplePaths[0], "/"+strings.Repeat("A", 100)+"/") || p.Over259 != len(deep) {
		t.Errorf("a long folder name was cut by the path budget, or the over-259 count is wrong: %s (%d over)", p.SamplePaths[0], p.Over259)
	}
	// The positive twin: a short destination keeps the full slug and nothing is over budget.
	short := cePlan(t, e, src, msgs)
	if got := len([]rune(slugOf(strings.TrimSuffix(filepath.Base(short.SamplePaths[0]), ".eml")))); got != 60 || short.Over259 != 0 {
		t.Errorf("a short destination has slug %d and %d paths over 259, want 60 and 0", got, short.Over259)
	}
}

// fatFS wraps the contained writer with FAT's directory limit: a directory holds at
// most 65,536 entries, and a long file name takes one entry for the file and one for
// each 13 characters of its name. A placement that would pass it is refused as a full
// directory (ENOSPC), as the filesystem does.
type fatFS struct {
	ceFS
	mu    sync.Mutex
	slots map[string]int
}

func (f *fatFS) Place(dir, tmp, final string) error {
	f.mu.Lock()
	f.slots[dir] += 1 + (len(final)+12)/13
	over := f.slots[dir] > 65536
	f.mu.Unlock()
	if over {
		return &os.LinkError{Op: "rename", Old: tmp, New: final, Err: syscall.ENOSPC}
	}
	return f.ceFS.Place(dir, tmp, final)
}

// covers: MA-519, R29, R4, R12
// Removable filesystems: a fault hook enforcing FAT's 65,536-entry directory limit is
// never reached by a 10,000-message folder, because no directory holds more than
// 1,000 files (the month splits as 2026-03, 2026-03~2, …); the positive twin is that
// every message is exported and each directory is within the bound.
func TestCopyExportFATDirectoryLimitIsNotReached(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	fat := &fatFS{slots: map[string]int{}}
	e.fsWrap = func(f ceFS) ceFS { fat.ceFS = f; return fat }
	msgs := ceMsgs(10000)
	for i := range msgs {
		msgs[i].Subject = longSubject
		msgs[i].Received = msgs[0].Received // one month: the directory split is what keeps it small
		msgs[i].SizeHint = 10
	}
	src.body = func(id string) []byte { return []byte("x" + id) }
	res := mustRun(t, e, cePlan(t, e, src, msgs), src)
	if res.Counts.Exported != 10000 || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("a 10,000-message folder on a FAT-limited directory: %s, %+v", res.Label, res.Counts)
	}
	dirs := 0
	for dir, slots := range fat.slots {
		if strings.HasPrefix(dir, "messages/") {
			dirs++
		}
		if slots > 65536 {
			t.Errorf("%s reached %d directory entries", dir, slots)
		}
	}
	if dirs != 10 {
		t.Errorf("10,000 messages in one month went to %d directories, want 10 of 1,000", dirs)
	}
	for dir, n := range countFilesPerDir(t, res.Root) {
		if n > copyFilesPerDir && strings.HasPrefix(dir, "messages") {
			t.Errorf("%s holds %d files, over %d", dir, n, copyFilesPerDir)
		}
	}
}

func countFilesPerDir(t *testing.T, root string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for p := range treeFiles(t, root) {
		out[filepath.ToSlash(filepath.Dir(p))]++
	}
	return out
}

// covers: MA-519, MA-439, R29, R4
// Modes are set only at creation: the engine's sources never call chmod, so a
// filesystem that rejects it (FAT, exFAT, SMB) still takes every write.
func TestCopyExportNeverChmods(t *testing.T) {
	files, err := filepath.Glob("copyexport*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no engine sources found: %v", err)
	}
	for _, f := range assure.Reached(t, files, "engine sources") {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if t2 := strings.TrimSpace(line); !strings.HasPrefix(t2, "//") && (strings.Contains(t2, "Chmod(") || strings.Contains(t2, "Fchmod(")) {
				t.Errorf("%s:%d changes a mode after creation: %s", f, i+1, t2)
			}
		}
	}
}

// covers: MA-519, R29, R4, R12
// EACCES at root creation is refused with R-w naming the folder and the remedy, and
// nothing is written: no export folder, no run record, no journal line. The positive
// twin first: the same destination, writable, takes the export.
func TestCopyExportRefusesAnUnwritableDestination(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("MA-519: root can create folders in a read-only directory; the CI legs run as a user")
	}
	e, src := newCEEnv(t), newFakeSrc()
	dest := filepath.Join(t.TempDir(), "dest")
	os.MkdirAll(dest, 0o755)
	if res := mustRun(t, e, cePlanAt(t, e, src, ceMsgs(1), dest), src); res.Counts.Exported != 1 {
		t.Fatalf("the positive twin exported %+v", res.Counts)
	}
	ro := filepath.Join(t.TempDir(), "ro")
	os.MkdirAll(ro, 0o755)
	p := cePlanAt(t, e, src, ceMsgs(1), ro)
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Skip("MA-519: this filesystem keeps no read-only directories")
	}
	defer os.Chmod(ro, 0o755)
	// Windows Chmod only toggles the read-only attribute, which does not stop a
	// directory from accepting new files; if the directory is still writable the OS
	// cannot stage this fault, so skip (the unwritable-destination refusal is
	// lab-verified on Windows, MA-519/README-RMM §D).
	if probe := filepath.Join(ro, ".probe"); os.WriteFile(probe, nil, 0o600) == nil {
		os.Remove(probe)
		t.Skip("MA-519: this OS does not make a directory unwritable via chmod (Windows); the refusal is lab-verified")
	}
	records, _ := os.ReadDir(e.recordsDir())
	_, err := ceRunPlan(t, e, p, src)
	var ref *CopyRefusal
	if !errors.As(err, &ref) {
		t.Fatalf("an unwritable destination did not refuse: %v", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(ro, "Controlled folder access", "Nothing was written"),
		assure.NoSideEffect(func() bool {
			ents, _ := os.ReadDir(ro)
			now, _ := os.ReadDir(e.recordsDir())
			return len(ents) == 0 && len(now) == len(records)
		}))
	if ref.ID != "R-w" {
		t.Errorf("the refusal is %s, want R-w", ref.ID)
	}
}
