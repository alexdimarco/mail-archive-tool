package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// covers: MA-103, R12, S9
// A missing -input path is refused BEFORE anything is created: no output dir
// (hence no lock, no last-run record, no attention sidecar) — a guaranteed
// failure must not first leave a half-built archive and a failed run behind
// (friction #2). The positive twin proves a healthy source exports through the
// same entry point, so the refusal fired for the right reason.
func TestMissingInputRefusesBeforeCreatingAnything(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	// Positive twin first: a real maildir source exports cleanly.
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	okOut := tmpDir(t)
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: okOut, Index: true}, logger, nil)
	if err != nil {
		t.Fatalf("healthy source failed: %v", err)
	}
	assure.Reached(t, res.Stats.Exported, "messages exported from a healthy source")

	// A missing -input path: the chosen output root must not exist afterwards.
	base := tmpDir(t)
	missing := filepath.Join(base, "no-such.pst")
	out := filepath.Join(base, "archive") // must NOT be created
	_, err = Run(context.Background(), Options{Inputs: []string{missing}, Out: out, Index: true}, logger, nil)
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(missing, "does not exist", "mounted"),
		assure.NoSideEffect(func() bool {
			// No output dir means no lock, no manifest, no last-run, no sidecar.
			_, e := os.Stat(out)
			return os.IsNotExist(e)
		}))
}

// covers: MA-106, R16
// The .ost advisory is a pure decision (OS, discovered paths, classic-Outlook
// present): on Windows, a live .ost among the auto-discovered inputs with
// classic Outlook available names -outlook up front; off Windows, with no .ost,
// or with no classic Outlook, it says nothing — so -auto on Linux is unchanged
// (friction #1).
func TestOSTAdvisory(t *testing.T) {
	paths := []string{"/home/u/mail.ost", "/home/u/archive.pst"}

	adv := ostAdvisory("windows", paths, true)
	assure.Reached(t, adv, "advisory for a windows .ost with classic Outlook")
	if !strings.Contains(adv, "-outlook") {
		t.Errorf("advisory does not name -outlook: %q", adv)
	}
	if adv := ostAdvisory("linux", paths, true); adv != "" {
		t.Errorf("advisory fired off Windows: %q", adv)
	}
	if adv := ostAdvisory("windows", []string{"/home/u/archive.pst"}, true); adv != "" {
		t.Errorf("advisory fired with no .ost among the inputs: %q", adv)
	}
	if adv := ostAdvisory("windows", paths, false); adv != "" {
		t.Errorf("advisory fired without classic Outlook (nothing to run -outlook): %q", adv)
	}
}

// covers: MA-108, R7
// Folder pages are regenerated only when a run exported something or the root
// index.html is missing: the first run creates it, a zero-change re-run leaves
// it (bytes untouched), and a run that adds a message rewrites it (nas-02).
func TestPagesRegeneratedOnlyOnChange(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}

	if _, err := Run(context.Background(), opts, logger, nil); err != nil {
		t.Fatal(err)
	}
	idxPath := filepath.Join(out, "index.html")
	if _, err := os.Stat(idxPath); err != nil {
		t.Fatalf("first run did not create index.html: %v", err)
	}

	// Plant a marker: a zero-change re-run that does not regenerate leaves it.
	marker := []byte("MARKER-DO-NOT-REGENERATE")
	if err := os.WriteFile(idxPath, marker, 0o644); err != nil {
		t.Fatal(err)
	}
	r2, err := Run(context.Background(), opts, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Stats.Exported != 0 {
		t.Fatalf("re-run exported %d, expected 0 (unchanged source)", r2.Stats.Exported)
	}
	if got, _ := os.ReadFile(idxPath); string(got) != string(marker) {
		t.Errorf("zero-change re-run regenerated index.html (the marker is gone)")
	}

	// A new message: index.html is regenerated (the marker is replaced).
	maildirMessages(t, src, 3) // adds one message (m2), rewrites the two unchanged ones
	r3, err := Run(context.Background(), opts, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r3.Stats.Exported != 1 {
		t.Fatalf("run with a new message exported %d, expected 1", r3.Stats.Exported)
	}
	if got, _ := os.ReadFile(idxPath); string(got) == string(marker) {
		t.Errorf("a run with a new message did not regenerate index.html")
	}
}

// covers: MA-109, R5
// effectiveEvery clamps the checkpoint cadence to [floor, 25000], scaling with
// the archive size (n/8): a small archive checkpoints every floor messages, a
// large one is capped so the durability writes never dominate a long run
// (nas-03). floor is the configured CheckpointEvery (default 1000).
func TestEffectiveEvery(t *testing.T) {
	cases := []struct{ floor, n, want int }{
		{1000, 0, 1000},       // empty archive → floor
		{1000, 4000, 1000},    // n/8 = 500 < floor
		{1000, 8000, 1000},    // n/8 = 1000 == floor
		{1000, 80000, 10000},  // n/8 = 10000 within bounds
		{1000, 400000, 25000}, // n/8 = 50000 capped
		{2, 6, 2},             // MA-88's small-archive shape stays at floor
	}
	for _, c := range cases {
		if got := effectiveEvery(c.floor, c.n); got != c.want {
			t.Errorf("effectiveEvery(%d, %d) = %d, want %d", c.floor, c.n, got, c.want)
		}
	}
}
