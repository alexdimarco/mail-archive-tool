package app

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/assure/leak"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// cleartext2bNames is cleartext2b's names, the tree grammar's only exemption.
func cleartext2bNames() map[string]bool {
	names := map[string]bool{}
	for n := range cleartext2b {
		names[n] = true
	}
	return names
}

// requireTree fails when out holds anything its mode's grammar does not allow.
func requireTree(t *testing.T, stage, out string, mode leak.Mode) {
	t.Helper()
	if vs := leak.CheckTree(out, mode, cleartext2bNames()); len(vs) > 0 {
		var lines []string
		for _, v := range vs {
			lines = append(lines, v.Path+": "+v.Why)
		}
		t.Fatalf("%s: an encrypted archive holds what it may not:\n%s", stage, strings.Join(lines, "\n"))
	}
}

// sealedWriteErr is a write fault the way a unit write reports one: naming an
// opaque unit, never the logical path (whose text would itself be mail metadata).
func sealedWriteErr(out string, errno syscall.Errno) error {
	return &fs.PathError{Op: "write", Path: filepath.Join(out, export.UnitsDir, "00", strings.Repeat("0", 32)), Err: errno}
}

// covers: MA-745, R37
// The sinks scan (§7 item 3): an encrypted archive's failing runs — a source file
// that cannot be read at a marked path, a run-fatal full drive, a held lock, another
// archive's key, a keyless scheduled refusal — each fail with a kind of the closed
// list, and nothing built for a sink names the mail: neither the text stderr and the
// Desktop show (ErrorTextFor) nor the run records and notice left in the archive.
// (The run's own log is a disclosed machine-local residual, §4 "run logs": it is
// never written inside the archive, MA-740, and is not scanned here.)
func TestAnEncryptedRunsMessagesNameNoMail(t *testing.T) {
	ms := leak.New()
	src, _ := leakStore(t, ms)
	bad := filepath.Join(filepath.Dir(src), ms[leak.Store]+"-"+ms[leak.Folder]+".pst")
	p14Write(t, bad, "not a pst file at all")
	type failing struct {
		name string
		run  func(out string, k Options) error
	}
	cases := []failing{
		{"an unreadable source file", func(out string, o Options) error {
			o.Inputs = []string{bad}
			_, err := Run(context.Background(), o, discard(), nil)
			return err
		}},
		{"a run-fatal full drive", func(out string, o Options) error {
			orig := exporterSetup
			defer func() { exporterSetup = orig }()
			exporterSetup = func(e *export.Exporter) {
				e.BeforeWrite = func(string) error { return sealedWriteErr(out, errFull) }
			}
			o.Inputs, o.Mode = []string{src}, export.Full // re-writes the archived message
			_, err := Run(context.Background(), o, discard(), nil)
			return err
		}},
		{"a held lock", func(out string, o Options) error {
			l, err := lockfile.Acquire(filepath.Join(out, lockfile.Name))
			if err != nil {
				return err
			}
			defer l.Release()
			o.Inputs = []string{src}
			_, err = Run(context.Background(), o, discard(), nil)
			return err
		}},
		{"another archive's key", func(out string, o Options) error {
			_, other := encryptedOut(t)
			o.Inputs, o.Key = []string{src}, other
			_, err := Run(context.Background(), o, discard(), nil)
			return err
		}},
		{"a keyless scheduled refusal", func(out string, o Options) error {
			o.Inputs, o.Key = []string{src}, nil
			_, err := Run(context.Background(), o, discard(), nil)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, k := encryptedOut(t)
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k}, discard(), nil); err != nil {
				t.Fatal(err)
			}
			err := c.run(out, Options{Out: out, Key: k, Unattended: true})
			if err == nil {
				t.Fatal("fixture: the run did not fail")
			}
			if kind := FailureKind(err); state.FailureText(kind) == state.FailureText("not a kind") && kind != state.FailRunFailed {
				t.Fatalf("the failure %v has no kind of the closed list (%q)", err, kind)
			}
			for _, h := range leak.ScanBytes("ErrorTextFor", []byte(ErrorTextFor(out, err)), ms) {
				t.Errorf("the text a sink shows names the mail: %s", h)
			}
			for _, f := range []string{state.LastRunName, state.RunHistoryName, state.AttentionName} {
				if b, rerr := os.ReadFile(filepath.Join(out, f)); rerr == nil {
					for _, h := range leak.ScanBytes(f, b, ms) {
						t.Errorf("the archive's %s names the mail: %s", f, h)
					}
				}
			}
		})
	}
}

// covers: MA-743, R37
// The tree grammar's own twin: a well-formed encrypted archive tree passes, and each
// thing an encrypted archive may not hold is reported — a stray root file, a folder
// other than units/, a shard that is not two hex characters, a unit in the wrong
// shard or of the wrong name, a file directly in units/, a temp at rest, a link — so
// "no violation" after a run is a statement about the run.
func TestTheTreeGrammarRefusesWhatItMust(t *testing.T) {
	unit := "ab" + strings.Repeat("0", 30)
	good := func(t *testing.T) string {
		out := tmpDir(t)
		p14Write(t, filepath.Join(out, export.UnitsDir, "ab", unit), "sealed")
		p14Write(t, filepath.Join(out, state.LastRunName), "{}")
		p14Write(t, filepath.Join(out, ".mailarchive-manifest.json"), "{}")
		return out
	}
	if vs := leak.CheckTree(good(t), leak.AtRest, cleartext2bNames()); len(vs) != 0 {
		t.Fatalf("a well-formed tree is reported: %v", vs)
	}
	for label, plant := range map[string]func(out string){
		"a stray root file":         func(out string) { p14Write(t, filepath.Join(out, "index.html"), "x") },
		"a folder other than units": func(out string) { p14Write(t, filepath.Join(out, "Inbox", "x.html"), "x") },
		"a shard that is not hex":   func(out string) { p14Write(t, filepath.Join(out, export.UnitsDir, "zz", "x"), "x") },
		"a unit in the wrong shard": func(out string) { p14Write(t, filepath.Join(out, export.UnitsDir, "cd", unit), "x") },
		"a unit of the wrong name":  func(out string) { p14Write(t, filepath.Join(out, export.UnitsDir, "ab", "ab-not-a-unit"), "x") },
		"a file directly in units":  func(out string) { p14Write(t, filepath.Join(out, export.UnitsDir, unit), "x") },
		"a temp at rest":            func(out string) { p14Write(t, filepath.Join(out, ".mailarchive-123.tmp"), "x") },
		"a link": func(out string) {
			if err := os.Symlink(tmpDir(t), filepath.Join(out, "README.txt")); err != nil {
				t.Skip("no symlinks here:", err)
			}
		},
	} {
		t.Run(label, func(t *testing.T) {
			out := good(t)
			plant(out)
			if vs := leak.CheckTree(out, leak.AtRest, cleartext2bNames()); len(vs) == 0 {
				t.Fatalf("%s is not reported", label)
			}
		})
	}
}

// covers: MA-743, R37
// The structural tree test (§7 item 4, the replacement guarantee for "one seam"):
// after every encrypted run, failure and refusal — local, -copy-first and Microsoft
// 365 runs with the index, pages and originals on, observed before every write and
// after the snapshot; an incremental re-run; an item write fault; a run-fatal full
// drive; another archive's key; an archive holding plaintext records; a keyless
// scheduled refusal; a held lock; a crashed run's leftovers, swept by the next run —
// every path under the archive matches the allow-list grammar: the vault and its
// mirror, the lock, the minimised run records and notices, the empty pause marker,
// the encrypted README, units/<hh>/<32 hex> named for their shard, and — in flight
// only — the sweepable temps; nothing else but the 2b list.
func TestEveryEncryptedPathLeavesOnlyTheAllowedTree(t *testing.T) {
	ms := leak.New()
	observations := 0
	outs := leakRuns(t, ms, true, func(stage, out string) {
		observations++
		requireTree(t, stage, out, leak.InFlight)
	})
	assure.Reached(t, observations, "mid-run observations")
	for _, out := range outs {
		requireTree(t, "after a run", out, leak.AtRest)
	}
	local := outs[0]
	src, _ := leakStore(t, ms)

	scenarios := map[string]func(t *testing.T) string{
		"an incremental re-run": func(t *testing.T) string {
			out, k := encryptedOut(t)
			for i := 0; i < 2; i++ {
				if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k, Index: true, Pages: true}, discard(), nil); err != nil {
					t.Fatal(err)
				}
			}
			return out
		},
		"an item write fault": func(t *testing.T) string {
			out, k := encryptedOut(t)
			orig := exporterSetup
			t.Cleanup(func() { exporterSetup = orig })
			exporterSetup = func(e *export.Exporter) {
				e.BeforeWrite = func(string) error { return sealedWriteErr(out, syscall.EACCES) }
			}
			_, _ = Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k, Index: true, Pages: true}, discard(), nil)
			return out
		},
		"a run-fatal full drive": func(t *testing.T) string {
			out, k := encryptedOut(t)
			orig := exporterSetup
			t.Cleanup(func() { exporterSetup = orig })
			exporterSetup = func(e *export.Exporter) {
				e.BeforeWrite = func(string) error { return sealedWriteErr(out, errFull) }
			}
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k, Index: true}, discard(), nil); err == nil {
				t.Fatal("fixture: a full drive did not fail the run")
			}
			return out
		},
		"another archive's key": func(t *testing.T) string {
			_, other := encryptedOut(t)
			_, _ = Run(context.Background(), Options{Inputs: []string{src}, Out: local, Key: other, Unattended: true}, discard(), nil)
			return local
		},
		"an archive holding plaintext records": func(t *testing.T) string {
			out := plainArchive(t)
			k, err := vaultCreate(out)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k, Unattended: true}, discard(), nil)
			// The refusal leaves the plaintext archive as it was: only what the refusal
			// itself writes is checked against the grammar here.
			for _, f := range []string{state.LastRunName, state.RunHistoryName, state.AttentionName} {
				if b, err := os.ReadFile(filepath.Join(out, f)); err == nil {
					for _, h := range leak.ScanBytes(f, b, ms) {
						t.Errorf("the refusal's %s names the mail: %s", f, h)
					}
				}
			}
			return ""
		},
		"a keyless scheduled refusal": func(t *testing.T) string {
			_, _ = Run(context.Background(), Options{Inputs: []string{src}, Out: local, Unattended: true}, discard(), nil)
			return local
		},
		"a held lock": func(t *testing.T) string {
			out, k := encryptedOut(t)
			l, err := lockfile.Acquire(filepath.Join(out, lockfile.Name))
			if err != nil {
				t.Fatal(err)
			}
			defer l.Release()
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k}, discard(), nil); err == nil {
				t.Fatal("fixture: a held lock did not refuse the run")
			}
			return out
		},
		"a crashed run's leftovers": func(t *testing.T) string {
			out, k := encryptedOut(t)
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k}, discard(), nil); err != nil {
				t.Fatal(err)
			}
			shards, _ := os.ReadDir(filepath.Join(out, export.UnitsDir))
			old := time.Now().Add(-time.Hour)
			for _, p := range []string{filepath.Join(out, ".mailarchive-123.tmp"), filepath.Join(out, export.UnitsDir, shards[0].Name(), ".mailarchive-456.tmp")} {
				if err := os.WriteFile(p, []byte("half a write"), 0o600); err != nil {
					t.Fatal(err)
				}
				os.Chtimes(p, old, old)
			}
			requireTree(t, "the crash leftovers", out, leak.InFlight)
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k}, discard(), nil); err != nil {
				t.Fatal(err)
			}
			return out
		},
	}
	ran := 0
	for name, sc := range scenarios {
		t.Run(name, func(t *testing.T) {
			if out := sc(t); out != "" {
				requireTree(t, name, out, leak.AtRest)
			}
			ran++
		})
	}
	if ran != len(scenarios) {
		t.Fatalf("%d of %d scenarios ran", ran, len(scenarios))
	}
}
