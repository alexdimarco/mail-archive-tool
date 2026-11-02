package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1012, R36
// The Desktop's plan route lists the encrypted archive's scratch folder (design-archive-delete
// §3.1 [C7, R3:NF-C7-OWN-READER]; slice 3a's conformance D1): a reader another program holds —
// serve or search — refuses at the plan, "Close MailArchive serve, search or other MailArchive
// windows for this archive first.", so nothing is logged and the archive this page has open
// stays open (before, the refusal came at step 1 (c), after the started line and after the page
// had locked the archive). Positive twin first: a reader of this Desktop's own process, the
// archive open on this page, does not refuse the plan (step 1 closes it).
func TestThePlanRouteRefusesAReaderHeldElsewhereBeforeLockingThePage(t *testing.T) {
	out := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	e.list(out)
	en, err := k.unlock(out, keysPassword, keyring.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scratch, err := app.ArchiveScratchFor(out, en.id)
	if err != nil {
		t.Fatal(err)
	}
	hold := func(pid int) func() {
		name := filepath.Join(scratch, "index", fmt.Sprintf("reader-%d-1.db", pid))
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(name, []byte("index"), 0o600)
		os.WriteFile(name+".lock", nil, 0o600)
		f, err := lockfile.HoldFile(name + ".lock")
		if err != nil {
			t.Fatal(err)
		}
		return func() { f.Close(); os.RemoveAll(filepath.Join(scratch, "index")) }
	}

	release := hold(os.Getpid())
	if p := e.plan(out); p["ok"] != true {
		t.Fatalf("positive twin: this page's own reader, the archive open here: %v", p)
	}
	release()

	release = hold(os.Getpid() + 1)
	defer release()
	m := e.plan(out)
	rc := 0
	if m["ok"] == false {
		rc = 1
	}
	assure.Refused(t, rc, replyErr(m), assure.Code(1),
		assure.Names("Close MailArchive serve, search or other MailArchive windows for this archive first."),
		assure.NoSideEffect(func() bool {
			return k.get(out) != nil && !strings.Contains(deletionsLogText(t, e.cfg), out)
		}))
}

// deletionsLogText is the Desktop's deletion log, "" when there is none yet.
func deletionsLogText(t *testing.T, cfg Config) string {
	t.Helper()
	b, _ := os.ReadFile(cfg.deletionsLogPath())
	return string(b)
}
