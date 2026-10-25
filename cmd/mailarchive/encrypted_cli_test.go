package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// cliArchive is an archive folder with a plaintext manifest; sealed adds a vault.
func cliArchive(t *testing.T, sealed bool) string {
	t.Helper()
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if sealed {
		p, err := vault.PrepareCreate(out, "cli test passphrase")
		if err != nil {
			t.Fatal(err)
		}
		k, err := p.Commit()
		if err != nil {
			t.Fatal(err)
		}
		k.Destroy()
	}
	return out
}

// filesUnder maps every path under dir to its bytes (a directory to a fixed marker),
// so "nothing written" catches a file rewritten in place, not only one added or removed.
func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			tree[p] = "<unreadable: " + err.Error() + ">"
			return nil
		}
		if p == dir {
			return nil
		}
		if d.IsDir() {
			tree[p] = "<dir>"
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			tree[p] = "<unreadable: " + rerr.Error() + ">"
			return nil
		}
		tree[p] = string(b)
		return nil
	})
	return tree
}

// covers: MA-733, MA-764, R37
// Every keyless command refuses an encrypted archive end-to-end through the built
// binary — export, -unattended export, graph, reindex, extract, search and serve —
// exiting non-zero with a message that names the folder and says it is encrypted,
// BEFORE its run log opens inside the archive (no -log file appears) and with
// nothing else written; a scheduled (-unattended) export's refusal is recorded as
// the failed last run. The positive twin: reindex on the same folder without a
// vault opens its -log there and says nothing of encryption. verify instead checks
// an encrypted archive without its key (tier 1, phase2b §4): it still refuses a
// -log inside the archive and -record, writing nothing, and otherwise reports the
// keyless outcome with its exit code.
func TestEveryKeylessCommandRefusesAnEncryptedArchive(t *testing.T) {
	env := append(os.Environ(), "HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir(), "AppData="+t.TempDir(), "LOCALAPPDATA="+t.TempDir())
	run := func(args ...string) (string, error) {
		cmd := exec.Command(testBin, args...)
		cmd.Env = env
		b, err := cmd.CombinedOutput()
		return string(b), err
	}

	plain := cliArchive(t, false)
	if msg, _ := run("reindex", "-out", plain, "-log", filepath.Join(plain, "run.log")); strings.Contains(msg, "encrypted") {
		t.Fatalf("the positive twin: a plaintext archive is refused as encrypted:\n%s", msg)
	}
	if _, err := os.Stat(filepath.Join(plain, "run.log")); err != nil {
		t.Fatalf("the positive twin: reindex on a plaintext archive opens its -log: %v", err)
	}

	input := t.TempDir()
	for name, args := range map[string]func(out string) []string{
		"export": func(out string) []string {
			return []string{"-input", input, "-out", out, "-log", filepath.Join(out, "run.log")}
		},
		"graph": func(out string) []string {
			return []string{"graph", "-auth", "device", "-tenant", "t", "-client-id", "c", "-mailbox", "a@example.org", "-out", out}
		},
		"reindex": func(out string) []string {
			return []string{"reindex", "-out", out, "-log", filepath.Join(out, "run.log")}
		},
		"extract": func(out string) []string {
			return []string{"extract", "-out", out, "-format", "eml", "-dest", filepath.Join(t.TempDir(), "x"), "-log", filepath.Join(out, "run.log")}
		},
		"search": func(out string) []string { return []string{"search", "-out", out, "anything"} },
		"serve": func(out string) []string {
			return []string{"serve", "-no-browser", "-addr", "127.0.0.1:0", "-out", out}
		},
	} {
		t.Run(name, func(t *testing.T) {
			out := cliArchive(t, true)
			before := assure.Reached(t, filesUnder(t, out), "the sealed archive's files")
			msg, err := run(args(out)...)
			rc := 0
			if err != nil {
				rc = 1
			}
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(out, "encrypted archive", "password or recovery key"),
				assure.NoSideEffect(func() bool { return reflect.DeepEqual(filesUnder(t, out), before) }))
		})
	}

	t.Run("a -log inside another encrypted archive", func(t *testing.T) {
		// covers the -log of every verb (they share one opener): a plaintext archive's
		// run whose -log is inside an encrypted archive refuses before the log opens.
		sealed := cliArchive(t, true)
		for _, verb := range []string{"reindex", "verify"} {
			plain := cliArchive(t, false)
			logPath := filepath.Join(sealed, "run.log")
			msg, err := run(verb, "-out", plain, "-log", logPath)
			rc := 0
			if err != nil {
				rc = 1
			}
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(sealed, "-log"), assure.NoSideEffect(func() bool {
				_, serr := os.Stat(logPath)
				return os.IsNotExist(serr)
			}))
		}
	})

	t.Run("verify checks it without the key", func(t *testing.T) {
		out := cliArchive(t, true)
		before := assure.Reached(t, filesUnder(t, out), "the sealed archive's files")
		code := func(err error) int {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode()
			}
			return 0
		}
		for _, refused := range []struct {
			args  []string
			names []string
		}{
			{[]string{"verify", "-out", out, "-log", filepath.Join(out, "run.log")}, []string{out, "-log"}},
			{[]string{"verify", "-record", "-out", out}, []string{out, "without -record"}},
		} {
			msg, err := run(refused.args...)
			assure.Refused(t, min(code(err), 1), msg, assure.Code(1), assure.Names(refused.names...),
				assure.NoSideEffect(func() bool { return reflect.DeepEqual(filesUnder(t, out), before) }))
		}
		// This folder's record file is a plaintext manifest beside a vault: damage.
		msg, err := run("verify", "-out", out)
		if code(err) != 2 || !strings.Contains(msg, "Keyless check of the encrypted archive "+out) || !strings.Contains(msg, "NOT intact: its record file holds a manifest that is not a sealed record") {
			t.Fatalf("keyless verify: exit %d\n%s", code(err), msg)
		}
		if lv, st, rerr := state.ReadLastVerify(out); rerr != nil || st != state.LastVerifyPresent || lv.Outcome != state.OutcomeDamaged || !lv.Encrypted {
			t.Fatalf("the check's record: %+v %v %v", lv, st, rerr)
		}
	})

	t.Run("unattended export", func(t *testing.T) {
		out := cliArchive(t, true)
		msg, err := run("-unattended", "-input", input, "-out", out)
		rc := 0
		if err != nil {
			rc = 1
		}
		lr, st, rerr := state.ReadLastRun(out)
		if rerr != nil || st != state.LastRunPresent || lr.Status != state.RunFailed || lr.ErrorKind != state.FailEncryptedNeedsKey || lr.Error != "" {
			t.Fatalf("a scheduled export's refusal is not the minimal recorded last run: %+v (%v)\n%s", lr, rerr, msg)
		}
		for _, name := range []string{state.LastRunName, state.RunHistoryName, state.AttentionName} {
			if raw, _ := os.ReadFile(filepath.Join(out, name)); strings.Contains(string(raw), out) || strings.Contains(string(raw), input) {
				t.Fatalf("%s names the archive or the source: %s", name, raw)
			}
		}
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(out, "encrypted archive"))
	})
}
