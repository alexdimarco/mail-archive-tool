package archdelete

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// heldBy makes the plan's probe (heldFn) report the files pick names as held by another program,
// counting every ask, until the test ends (fault injection: Windows alone refuses such a file).
func heldBy(t *testing.T, pick func(rel string) bool) *int {
	t.Helper()
	asks := 0
	old := heldFn
	t.Cleanup(func() { heldFn = old })
	heldFn = func(_ *os.Root, rel string) bool {
		asks++
		return pick(filepath.ToSlash(rel))
	}
	return &asks
}

// covers: MA-1039, R36
// The probe asks only the folder planned and only its first heldProbeMax files (the settle pass's
// LOW-7: no test caught either removed): a folder whose identity is no longer the plan's is asked
// nothing, and a plan of 250 files asks 200. Positive twin first: the folder planned, with few
// files, is asked about each.
func TestTheHeldProbeAsksOnlyThePlannedFolderAndItsFirstFiles(t *testing.T) {
	asks := heldBy(t, func(string) bool { return true })
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	id, err := rootIdentity(root)
	root.Close()
	if err != nil {
		t.Fatal(err)
	}
	items := func(n int) []Item {
		var out []Item
		for i := 0; i < n; i++ {
			out = append(out, Item{Rel: "f" + strings.Repeat("x", i%7) + ".html"})
		}
		return out
	}
	// Positive twin: the folder planned, three files, three asks.
	if got := heldFiles(&DeletePlan{Folder: dir, Identity: id, Remove: items(3)}); len(got) != 3 || *asks != 3 {
		t.Fatalf("the twin: held %d, %d asks", len(got), *asks)
	}
	*asks = 0
	if got := heldFiles(&DeletePlan{Folder: dir, Identity: id, Remove: items(250)}); len(got) != heldProbeMax || *asks != heldProbeMax || heldProbeMax != 200 {
		t.Fatalf("250 files: held %d, %d asks (max %d)", len(got), *asks, heldProbeMax)
	}
	*asks = 0
	other := id
	other.FileID++
	if got := heldFiles(&DeletePlan{Folder: dir, Identity: other, Remove: items(3)}); len(got) != 0 || *asks != 0 {
		t.Fatalf("another folder now: held %d, %d asks", len(got), *asks)
	}
}

// covers: MA-1039, R36
// A plan that finishes a delete which stopped part way says when a file of it is still held open
// by another program, so this run would stop at it again (the 1.5.3 re-walk's N17: a re-run made
// while Outlook still held the file said nothing until it failed). Positive twins first: with the
// file released the plan says nothing of it, and an ordinary delete's plan asks nothing.
func TestARerunPlanWarnsOfAFileStillHeld(t *testing.T) {
	held := true
	asks := heldBy(t, func(rel string) bool { return held && strings.HasSuffix(rel, ".html") })

	// Positive twin: an ordinary plan asks nothing.
	twin := newRig(t, smallArchive(t, t.TempDir()))
	if p := twin.plan(t); len(p.Held) != 0 || p.HeldLine() != "" || *asks != 0 {
		t.Fatalf("an ordinary plan: held %v, line %q, %d asks", p.Held, p.HeldLine(), *asks)
	}

	dir := smallArchive(t, t.TempDir())
	writeFile(t, dir+"/README.txt", "the archive's readme")
	r := newRig(t, dir)
	faultOn(t, nil, func(rel string) error {
		if held && strings.HasSuffix(rel, ".html") {
			return errors.New("The process cannot access the file")
		}
		return nil
	})
	if res := r.run(t, false); res.Status != StatusPartial {
		t.Fatalf("the first run: %s", res.Message())
	}

	// Positive twin: released, nothing said.
	held = false
	if p := r.plan(t); p.Finishes == nil || len(p.Held) != 0 || p.HeldLine() != "" {
		t.Fatalf("released: held %v, line %q", p.Held, p.HeldLine())
	}

	held = true
	p := r.plan(t)
	want := "1 file this delete must remove is still in use by another program: " + stemA + ".html. Close Outlook or any program using it before you delete, or this delete will stop at it again."
	if len(p.Held) != 1 || p.HeldLine() != want {
		t.Fatalf("held: %v, line %q, want %q", p.Held, p.HeldLine(), want)
	}
	p.Held = append(p.Held, "Mailbox/Sent/x.html")
	if got := p.HeldLine(); !strings.HasPrefix(got, "2 files this delete must remove are still in use by another program: ") || !strings.Contains(got, "using them") {
		t.Fatalf("two held: %q", got)
	}
}
