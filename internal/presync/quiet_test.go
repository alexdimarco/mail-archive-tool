package presync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-555
// The quiet detector embodies the revision-3 rule (design section 4.4.2, lab
// L-M5): quiet needs a FIRST observed change before it may conclude, so the
// ~10-minute opening silence of Thunderbird's first offline poll never ends the
// session early. It folds growth, mtime-only touches, new files, renames and
// deletions into its change signal without ever reading content, a change still
// in flight at maxWait is not-quiet, and a suspend gap resets the clock.
func TestPresyncQuietDetector(t *testing.T) {
	base := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	cfg := quietConfig{settle: 60 * time.Second, quiet: 180 * time.Second, maxWait: 1200 * time.Second}

	t.Run("opening silence never quiets before maxWait (the rev-3 fix)", func(t *testing.T) {
		q := newQuietTracker(base, cfg)
		for s := 0; s < 1200; s += 10 {
			now := base.Add(time.Duration(s) * time.Second)
			q.observe(0x1111, now) // the same digest every poll: nothing changed
			if o, done := q.verdict(now); done {
				t.Fatalf("at +%ds the session ended %q, but with no change it must wait to maxWait (a settle+quiet quit here is the L-M5 bug)", s, o.Name)
			}
		}
		now := base.Add(1200 * time.Second)
		q.observe(0x1111, now)
		if o, done := q.verdict(now); !done || o != OutcomeNoActivity {
			t.Fatalf("at maxWait with no change: got (%q, done=%v), want ok-noactivity", o.Name, done)
		}
	})

	t.Run("a change then quiet ends ok", func(t *testing.T) {
		q := newQuietTracker(base, cfg)
		q.observe(0x1111, base.Add(10*time.Second))  // baseline
		q.observe(0x2222, base.Add(600*time.Second)) // the download lands at +10 min
		if o, done := q.verdict(base.Add(700 * time.Second)); done {
			t.Fatalf("only +100s after the change the session ended %q; quiet is %v", o.Name, cfg.quiet)
		}
		now := base.Add(600*time.Second + cfg.quiet)
		if o, done := q.verdict(now); !done || o != OutcomeOK {
			t.Fatalf("quiet after the last change: got (%q, done=%v), want ok", o.Name, done)
		}
	})

	t.Run("still changing at maxWait is not-quiet", func(t *testing.T) {
		q := newQuietTracker(base, cfg)
		q.observe(0x1111, base)
		for s := 10; s <= 1200; s += 10 { // a fresh digest every poll: never settles
			q.observe(uint64(s), base.Add(time.Duration(s)*time.Second))
		}
		now := base.Add(1200 * time.Second)
		if o, done := q.verdict(now); !done || o != OutcomeNotQuiet {
			t.Fatalf("still changing at maxWait: got (%q, done=%v), want not-quiet", o.Name, done)
		}
	})

	t.Run("a suspend gap resets the quiet clock", func(t *testing.T) {
		q := newQuietTracker(base, cfg)
		q.observe(0x1111, base)
		q.observe(0x2222, base.Add(30*time.Second))  // a change at +30s
		q.observe(0x2222, base.Add(400*time.Second)) // same digest, but a 370s gap => suspend reset
		if o, done := q.verdict(base.Add(400*time.Second + cfg.quiet - time.Second)); done {
			t.Fatalf("after a suspend reset the session ended %q before quiet elapsed again", o.Name)
		}
		if o, done := q.verdict(base.Add(400*time.Second + cfg.quiet)); !done || o != OutcomeOK {
			t.Fatalf("quiet after the suspend reset: got (%q, done=%v), want ok", o.Name, done)
		}
	})

	t.Run("digest folds metadata, never content, and skips symlinks", func(t *testing.T) {
		dir := t.TempDir()
		inbox := filepath.Join(dir, "INBOX")
		writeFile(t, inbox, "aaaa")
		d1, trunc := digestWatch([]string{dir})
		if trunc {
			t.Fatalf("small fixture reported truncated")
		}
		info, err := os.Stat(inbox)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		writeFile(t, inbox, "bbbb") // same 4 bytes, new content
		if err := os.Chtimes(inbox, info.ModTime(), info.ModTime()); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		if d2, _ := digestWatch([]string{dir}); d2 != d1 {
			t.Errorf("digest moved on a same-size same-mtime content edit: the walk read content")
		}
		writeFile(t, inbox, "aaaaaa") // growth
		if d3, _ := digestWatch([]string{dir}); d3 == d1 {
			t.Errorf("digest unchanged after the mailbox grew")
		}
		real := filepath.Join(dir, "real")
		if err := os.Mkdir(real, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeFile(t, filepath.Join(real, "x"), "zzzz")
		before, _ := digestWatch([]string{dir})
		if err := os.Symlink(real, filepath.Join(dir, "link")); err != nil {
			t.Skipf("symlink unsupported here: %v", err)
		}
		if after, _ := digestWatch([]string{dir}); after != before {
			t.Errorf("digest moved after adding a symlinked directory: the walk followed it")
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// covers: MA-555
// The walk's entry cap counts every visited node, so a directory-heavy tree
// (nested directories, no regular files) still trips the cap and reports
// truncation — not only a file-heavy tree (adversarial-pass F1).
func TestDigestWatchCapCountsDirectories(t *testing.T) {
	orig := watchMaxEntries
	defer func() { watchMaxEntries = orig }()
	watchMaxEntries = 3

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b", "c", "d", "e"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, truncated := digestWatch([]string{root}); !truncated {
		t.Error("a directory-heavy tree did not trip the entry cap (directories must count)")
	}
}
