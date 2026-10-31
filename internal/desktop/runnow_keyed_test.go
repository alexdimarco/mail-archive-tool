package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/assure/leak"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The encrypted Run now with the Thunderbird sync, and its Lock window
// (design-presync-attended §3.2, §7, §8, §9, slice AS8; owner rulings 4, OQ-3, OQ-4,
// OQ-8). Thunderbird is the external binary, so the session is the scripted fake of
// runnow_attended_test.go through presyncjob's seam (C-41) — the mock CLAUDE.md
// allows; the rest is real: the Desktop's handlers, its key registry and leases, the
// wizard's RunJobInProcessKeyed and app.Run over a real encrypted archive. No real
// Thunderbird is started, and the home and config folders are the test's own.

// encRig is a Desktop whose current archive is encrypted, unlocked here, with its
// Run now job (the sync on or off) made from a Thunderbird profile holding one new
// message, and the scripted session. It records Run now's leases and the keys the
// runs were handed.
type encRig struct {
	t     *testing.T
	d     *dashboard
	out   string
	jp    string
	life  *Lifecycle
	s     *tbScript
	goos  string
	store graphconfig.SecretStore

	mu     sync.Mutex
	leases []*keyring.Unlocked
	keys   []*vault.Key
}

// encryptedFor makes an encrypted archive holding one message; with index false its
// first backup keeps no search index, so the next indexed run catches the index up
// from the sealed records first (the sealed catch-up, app/run.go).
func encryptedFor(t *testing.T, index bool) string {
	t.Helper()
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "Enc")
	p, err := keyring.Create(out, keysPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	box := filepath.Join(t.TempDir(), "Inbox")
	os.WriteFile(box, []byte(keysMbox), 0o600)
	opts := app.Options{Inputs: []string{box}, Out: out, Index: index, KeepRaw: true}
	done, err := keyring.Apply(&opts, u)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if _, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	return out
}

// thunderbirdStore is a mail folder under a Thunderbird profile (a prefs.js at its
// root) holding laterMbox, a message the archive does not have yet.
func thunderbirdStore(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte("// prefs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(profile, "Mail", "Local Folders")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "Inbox"), []byte(laterMbox), 0o644); err != nil {
		t.Fatal(err)
	}
	return store
}

type encRigOpts struct {
	goos         string
	startAllowed bool
	sync         bool
	noIndex      bool // the archive's first backup kept no index: Run now catches it up first
	stateDir     *string
}

func newEncRig(t *testing.T, o encRigOpts) *encRig {
	t.Helper()
	isolateHome(t)
	out := encryptedFor(t, !o.noIndex)
	name := schedule.DefaultNameFor(out)
	jp, err := job.PathFor(name)
	if err != nil {
		t.Fatal(err)
	}
	j := job.Job{Name: name, Inputs: []string{thunderbirdStore(t)}, Out: out, Mode: "incremental", KeepRaw: true}
	if o.sync {
		j.ThunderbirdSync = &job.ThunderbirdSync{}
	}
	if err := job.Write(jp, j); err != nil {
		t.Fatal(err)
	}
	g := &encRig{t: t, out: out, jp: jp, s: newTBScript(), goos: o.goos}
	dir := filepath.Join(t.TempDir(), "presync")
	if o.stateDir != nil {
		dir = *o.stateDir
	}
	t.Cleanup(presyncjob.OverrideSessionForTest(g.s.run, dir))
	t.Cleanup(presync.OverrideTablesForTest(nil, map[string]bool{o.goos: o.startAllowed}))
	origLease, origApply := runLease, runApply
	runLease = func(e *keyEntry, oo keyring.OpenOptions) (*keyring.Unlocked, error) {
		l, err := origLease(e, oo)
		g.mu.Lock()
		g.leases = append(g.leases, l)
		g.mu.Unlock()
		return l, err
	}
	runApply = func(opts *app.Options, u *keyring.Unlocked) (func(), error) {
		done, err := origApply(opts, u)
		if err == nil {
			g.mu.Lock()
			g.keys = append(g.keys, opts.Key)
			g.mu.Unlock()
		}
		return done, err
	}
	t.Cleanup(func() { runLease, runApply = origLease, origApply })
	g.life = NewLifecycle(context.Background())
	t.Cleanup(g.life.Stop)
	settings := filepath.Join(t.TempDir(), "s.json")
	SaveSettings(settings, Settings{Out: out})
	g.store = graphconfig.NewFileSecretStore(t.TempDir())
	keys := newArchiveKeys(g.store, readerLockedNote)
	t.Cleanup(func() { keys.lockAll(true) })
	g.d = newDashboard(Config{Out: out, GOOS: o.goos, SettingsPath: settings, Store: g.store, Keys: keys, Lifecycle: g.life})
	g.unlock()
	return g
}

func (g *encRig) unlock() {
	g.t.Helper()
	if m := callAction(g.t, g.d, g.d.unlockAction, map[string]any{"secret": keysPassword}); m["ok"] != true {
		g.t.Fatalf("unlock: %v", m)
	}
}

func (g *encRig) lock() map[string]any {
	g.t.Helper()
	m := callAction(g.t, g.d, g.d.lockAction, nil)
	if m["ok"] != true {
		g.t.Fatalf("Lock: %v", m)
	}
	return m
}

func (g *encRig) start() {
	g.t.Helper()
	if m := callAction(g.t, g.d, g.d.runBackup, nil); m["ok"] != true {
		g.t.Fatalf("Run now did not start: %v", m)
	}
}

func (g *encRig) activity() map[string]any {
	rec := httptest.NewRecorder()
	g.d.activity(rec, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return m
}

func (g *encRig) done() map[string]any {
	g.t.Helper()
	for i := 0; i < 1500; i++ {
		if m := g.activity(); m["running"] != true {
			return m
		}
		time.Sleep(20 * time.Millisecond)
	}
	g.t.Fatalf("the Run now never ended: %v", g.activity())
	return nil
}

func (g *encRig) at(phase string) {
	g.t.Helper()
	for {
		select {
		case p := <-g.s.at:
			if p == phase {
				return
			}
		case <-time.After(30 * time.Second):
			g.t.Fatalf("the session never reached %q", phase)
		}
	}
}

func (g *encRig) status() string {
	lr, _, err := state.ReadLastRun(g.out)
	if err != nil {
		g.t.Fatal(err)
	}
	return lr.Status
}

// windowOpen reports whether a Lock window is open: the slot's, or a click's still
// waiting for the slot.
func (g *encRig) windowOpen() bool {
	g.d.cap.mu.Lock()
	defer g.d.cap.mu.Unlock()
	return g.d.cap.lockWin != nil || len(g.d.cap.pendingWins) > 0
}

// lastLease and lastKey are the newest Run now's lease and the key its run was handed.
func (g *encRig) lastLease() *keyring.Unlocked {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.leases) == 0 {
		g.t.Fatal("Run now took no lease")
	}
	return g.leases[len(g.leases)-1]
}

func (g *encRig) lastKey() *vault.Key {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.keys) == 0 {
		return nil
	}
	return g.keys[len(g.keys)-1]
}

// keyUsable reports whether k can still be used: a destroyed key refuses every use.
func keyUsable(k *vault.Key) bool {
	_, err := k.UnitName(vault.UnitMessage, "probe")
	return err == nil
}

// nothingWasRead proves the cancelled run read no mail: unlocked again, a Run now
// with the session released adds the one new message (a run that had read it would
// leave nothing to add).
func (g *encRig) nothingWasRead() bool {
	g.t.Helper()
	g.unlock()
	pauses := runNowPause
	runNowPause = func(string) {}
	defer func() { runNowPause = pauses }()
	select {
	case g.s.release <- struct{}{}:
	default:
	}
	g.start()
	m := g.done()
	return strings.Contains(str(m["note"])+str(m["result"]), "Backup complete: 1 message(s) added") && g.status() == state.RunOK
}

// pauseAt sets the Run now's seam to call fn at the named step.
func pauseRunNowAt(t *testing.T, point string, fn func()) {
	t.Helper()
	orig := runNowPause
	runNowPause = func(p string) {
		if p == point && fn != nil {
			fn()
		}
	}
	t.Cleanup(func() { runNowPause = orig })
}

// covers: MA-913, R37, R32, S64
// A Lock cancels an encrypted Run now at any point before it starts reading mail, and
// clears its key; once reading has started it goes on with its own key (design-
// presync-attended §3.2, §9, owner rulings 4, OQ-3, OQ-8; C-5/T8, C-10, C-11, C-13,
// C-21, C-75, C-76, C-85). Positive twin first: an encrypted sync Run now, released,
// syncs and backs up. A Lock while it waits for Thunderbird: the reply says the
// backup was cancelled before it read any mail, its key is being cleared once its
// current step ends, and Thunderbird, which it started out of sight, was left open;
// the session's Stop ended and its cause is not Cancel's (so nothing is asked to
// quit); the lease is locked at once while the key the run holds is not destroyed
// until the run lets go of it, and is destroyed once it returns; the record reads
// cancelled and nothing was read. The reply's note stays on screen (no reload).
func TestALockCancelsAnEncryptedRunNowWhileItWaits(t *testing.T) {
	g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	g.start()
	g.at(presync.PhaseWaiting)
	if !g.windowOpen() {
		t.Fatal("twin: no Lock window while an encrypted Run now waits")
	}
	g.s.release <- struct{}{}
	m := g.done()
	if g.status() != state.RunOK || !strings.HasPrefix(str(m["note"]), "Backup complete: ") || !strings.Contains(str(m["note"]), "\nThunderbird synced: its mail stopped arriving after 6 min.") || g.windowOpen() {
		t.Fatalf("twin: the released encrypted Run now: %v, record %s, window open %v", m, g.status(), g.windowOpen())
	}
	cfg, _, _ := g.s.last()
	if !cfg.Attended || cfg.Stop == nil || cfg.EndWait == nil {
		t.Fatalf("twin: the encrypted Run now's session was not attended: %+v", cfg)
	}

	g = newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	var heldDuringStop, leaseLockedDuringStop bool
	g.s.onStop = func() {
		k := g.lastKey()
		heldDuringStop = k != nil && keyUsable(k)
		_, err := g.lastLease().Lease()
		leaseLockedDuringStop = errors.Is(err, keyring.ErrLocked)
	}
	g.start()
	g.at(presync.PhaseWaiting)
	reply := g.lock()
	want := lockCancelledText + " Thunderbird, which it started out of sight, was left open — quit it from the Dock before the next Run now."
	if str(reply["note"]) != want {
		t.Errorf("the Lock reply %q, want %q", reply["note"], want)
	}
	g.done()
	_, cause, stopped := g.s.last()
	if !stopped || errors.Is(cause, presync.ErrCancelAndQuit) || !errors.Is(cause, errLockedDuringSync) {
		t.Errorf("a Lock delivered cause %v, Stop ended %v; want errLockedDuringSync and the Stop ended", cause, stopped)
	}
	if !heldDuringStop || !leaseLockedDuringStop {
		t.Errorf("while the session still ran: the run's key usable %v (want true), the lease locked %v (want true)", heldDuringStop, leaseLockedDuringStop)
	}
	if k := g.lastKey(); k == nil || keyUsable(k) {
		t.Error("the run's key outlived the run after a Lock")
	}
	if g.status() != state.RunCancelled || g.windowOpen() {
		t.Errorf("after a Lock: record %s, window open %v; want cancelled, closed", g.status(), g.windowOpen())
	}
	if !g.nothingWasRead() {
		t.Error("the Lock-cancelled run read mail")
	}
	// The note stays on screen with a Done button; the page never reloads it away.
	if strings.Contains(encryptionJS, "setTimeout(()=>location.reload(),6000)") || !strings.Contains(encryptionJS, "if(d.ok&&d.note){done(true);noteWithDone(d.note);return;}") ||
		!strings.Contains(encryptionJS, "b.addEventListener('click',()=>location.reload())") {
		t.Error("the Lock reply's note does not stay on screen with a Done button")
	}
}

// covers: MA-913, R37, R32, S64
// A Lock before the session starts Thunderbird — during the heal, and once the key is
// handed to the run, before its discovery and decrypting open — starts nothing and
// reads nothing, for a sync Run now and for one with no sync (OQ-8): the reply says
// the backup was cancelled and nothing about Thunderbird. Once the key was handed
// over, the run is recorded cancelled; a Lock during the heal comes before the run's
// first use of the key, so no run begins and the last record is left as it was. A
// Lock after Thunderbird's wait and before read start — during the sealed catch-up,
// through app's pause seam — is cancelled at the next check, nothing read, and the
// reply says the key is cleared once the current step ends, then that Thunderbird was
// left open; a Run now with no sync, the same with no Thunderbird sentence.
func TestALockBeforeReadingCancelsEveryEncryptedRunNow(t *testing.T) {
	for _, sync := range []bool{true, false} {
		for _, point := range []string{"heal", "apply"} {
			g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: sync})
			before := g.status()
			var reply map[string]any
			pauseRunNowAt(t, point, func() { reply = g.lock() })
			g.start()
			g.done()
			runNowPause = func(string) {}
			if g.s.calls() != 0 {
				t.Errorf("sync %v, Lock at %s: the session started %d times", sync, point, g.s.calls())
			}
			if str(reply["note"]) != lockCancelledText {
				t.Errorf("sync %v, Lock at %s: the reply %q, want %q", sync, point, reply["note"], lockCancelledText)
			}
			wantStatus := state.RunCancelled
			if point == "heal" {
				wantStatus = before
			}
			if g.status() != wantStatus || g.windowOpen() {
				t.Errorf("sync %v, Lock at %s: record %s (want %s), window open %v", sync, point, g.status(), wantStatus, g.windowOpen())
			}
			if !g.nothingWasRead() {
				t.Errorf("sync %v, Lock at %s: the run read mail", sync, point)
			}
		}

		// The sealed catch-up, after the hook and before read start.
		g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: sync, noIndex: true})
		var reply map[string]any
		var points []string
		restore := app.OverrideRunPauseForTest(func(p string) {
			points = append(points, p)
			if p == "sealed-catch-up" {
				reply = g.lock()
			}
		})
		if sync {
			g.s.release <- struct{}{} // the mail settles at once
		}
		g.start()
		g.done()
		restore()
		want := lockCancelledText
		if sync {
			want += " Thunderbird, which it started out of sight, was left open — quit it from the Dock before the next Run now."
		}
		if len(points) == 0 || points[len(points)-1] != "sealed-catch-up" {
			t.Fatalf("sync %v: the run never reached the sealed catch-up: %v", sync, points)
		}
		if str(reply["note"]) != want || !strings.Contains(want, "once its current step ends") {
			t.Errorf("sync %v, Lock during the catch-up: the reply %q, want %q", sync, reply["note"], want)
		}
		if g.status() != state.RunCancelled || g.windowOpen() {
			t.Errorf("sync %v, Lock during the catch-up: record %s, window open %v", sync, g.status(), g.windowOpen())
		}
		if !g.nothingWasRead() {
			t.Errorf("sync %v, Lock during the catch-up: the run read mail", sync)
		}
	}
}

// covers: MA-913, R37, S64
// Once reading has started the Lock does not stop the run (phase 3 §3.2, C-13, OQ-3):
// a Lock that lands right after the window closed at read start — the race through
// the seam between the window's close and app.Run's second check — is told the backup
// has started reading mail, finishes with its own key, then the key is cleared, and
// that Cancel stops it now; the run goes on and adds the message. A Lock just before
// read start (the index open) lands on the other side and cancels it. The idle lock
// and a switch away during the wait never stop it. A Lock of another archive does not
// reach it.
func TestALockAfterReadStartLetsTheRunFinish(t *testing.T) {
	g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	var reply map[string]any
	pauseRunNowAt(t, "read-start", func() { reply = g.lock() })
	g.s.release <- struct{}{}
	g.start()
	m := g.done()
	runNowPause = func(string) {}
	if str(reply["note"]) != lockReadingText {
		t.Errorf("a Lock after read start: the reply %q, want %q", reply["note"], lockReadingText)
	}
	if g.status() != state.RunOK {
		t.Errorf("a Lock after read start stopped the run: record %s (%v)", g.status(), m)
	}

	// The other side of the race: a Lock during the index open, before read start.
	g = newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	restore := app.OverrideRunPauseForTest(func(p string) {
		if p == "index-open" {
			reply = g.lock()
		}
	})
	g.s.release <- struct{}{}
	g.start()
	g.done()
	restore()
	if !strings.HasPrefix(str(reply["note"]), lockCancelledText) || g.status() != state.RunCancelled {
		t.Errorf("a Lock during the index open: reply %q, record %s; want cancelled", reply["note"], g.status())
	}

	// The idle lock, a switch away, and a Lock of another archive do not stop it.
	g = newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	g.start()
	g.at(presync.PhaseWaiting)
	g.d.cfg.Keys.reap(time.Now().Add(24 * time.Hour)) // the idle lock
	other := filepath.Join(t.TempDir(), "Other")
	if n := g.d.cap.stopSyncWait(other, "darwin"); n != "" {
		t.Errorf("a Lock of another archive reached this Run now: %q", n)
	}
	g.d.cfg.Keys.lockExcept(other) // a switch away locks the archive left
	time.Sleep(100 * time.Millisecond)
	if _, _, stopped := g.s.last(); stopped || g.activity()["running"] != true {
		t.Fatal("the idle lock or a switch stopped the Run now")
	}
	g.s.release <- struct{}{}
	g.done()
	if g.status() != state.RunOK {
		t.Errorf("after an idle lock and a switch: record %s, want ok", g.status())
	}
}

// covers: MA-906, MA-913
// A Run now whose activity a Lock or a switch away cleared (C-15) never ends saying
// "Backup finished." (gate-2 adversarial pass 1 over AS4, as4web-1): its result was
// cleared, so its end note says only that it ended and why its result is not shown.
// Positive twin first: an encrypted Run now cancelled after read start, with no Lock,
// ends with the cancel words. Then a Lock after read start ("Locked for new use…"),
// followed by Cancel: the record reads cancelled and the end note is the cleared-result
// note, with no result and no error. And a switch away while it waits, then Cancel:
// back on its archive, the same note.
func TestAClearedRunNowNeverEndsAsFinished(t *testing.T) {
	g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	pauseRunNowAt(t, "read-start", func() { callAction(t, g.d, g.d.cancelJobAction, nil) })
	g.s.release <- struct{}{}
	g.start()
	m := g.done()
	if n := str(m["note"]); !strings.HasPrefix(n, cancelledText+"\nThunderbird synced: its mail stopped arriving after 6 min.") || g.status() != state.RunCancelled {
		t.Fatalf("twin: a Run now cancelled after read start: note %q, record %s", m["note"], g.status())
	}

	var reply map[string]any
	g = newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	pauseRunNowAt(t, "read-start", func() {
		reply = g.lock()
		callAction(t, g.d, g.d.cancelJobAction, nil)
	})
	g.s.release <- struct{}{}
	g.start()
	m = g.done()
	if str(reply["note"]) != lockReadingText || g.status() != state.RunCancelled {
		t.Fatalf("the Lock after read start, then Cancel: reply %q, record %s", reply["note"], g.status())
	}
	if str(m["note"]) != runNowMutedEndNote || str(m["result"]) != "" || str(m["error"]) != "" || strings.Contains(str(m["note"]), "Backup finished.") {
		t.Errorf("a Locked Run now that was cancelled ends with note %q (result %q, error %q), want %q",
			m["note"], m["result"], m["error"], runNowMutedEndNote)
	}

	g = newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	pauseRunNowAt(t, "read-start", nil)
	g.start()
	g.at(presync.PhaseWaiting)
	SaveSettings(g.d.cfg.SettingsPath, Settings{Out: t.TempDir()}) // a switch away
	g.activity()
	callAction(t, g.d, g.d.cancelJobAction, nil)
	g.done()
	SaveSettings(g.d.cfg.SettingsPath, Settings{Out: g.out}) // and back
	if m = g.activity(); str(m["note"]) != runNowMutedEndNote || str(m["result"]) != "" {
		t.Errorf("a switched-away Run now that was cancelled ends with note %q (result %q), want %q", m["note"], m["result"], runNowMutedEndNote)
	}
}

// covers: MA-913, R37
// The window can never go stale (C-75, C-76): a Run now refused in
// RunJobInProcessKeyed (an Outlook job), one that fails opening the archive, one with
// no sync, one whose sync could not be prepared (not-set-up), and one whose session
// panics each leave no window once they return, and a Lock afterwards claims no
// cancel. Positive twin first: while a Run now waits, the window is open.
func TestNoStaleLockWindow(t *testing.T) {
	g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	g.start()
	g.at(presync.PhaseWaiting)
	if !g.windowOpen() {
		t.Fatal("twin: no window while it waits")
	}
	g.s.release <- struct{}{}
	g.done()

	check := func(label string, g *encRig, fails bool) {
		t.Helper()
		g.start()
		m := g.done()
		if fails && (str(m["error"]) == "" || g.s.calls() != 0) {
			t.Errorf("%s: fixture: the run did not fail before its sync (%v, %d sessions)", label, m, g.s.calls())
		}
		if g.windowOpen() {
			t.Errorf("%s: a window is left open", label)
		}
		if n := str(g.lock()["note"]); strings.Contains(n, "cancelled") {
			t.Errorf("%s: a Lock afterwards claimed a cancel: %q", label, n)
		}
	}

	outlook := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	j, err := job.Read(outlook.jp)
	if err != nil {
		t.Fatal(err)
	}
	j.Outlook = true
	if err := job.Write(outlook.jp, j); err != nil {
		t.Fatal(err)
	}
	check("an Outlook job, refused", outlook, true)

	broken := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	for _, n := range []string{container.PointerName, container.MirrorName} {
		ptr := filepath.Join(broken.out, n)
		if _, err := os.Stat(ptr); err != nil {
			t.Fatalf("fixture: the sealed record's pointer: %v", err)
		}
		os.WriteFile(ptr, []byte("not a pointer"), 0o600)
	}
	broken.s.release <- struct{}{}
	check("a failure opening the archive", broken, true)

	check("a job with no sync", newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: false}), false)

	empty := ""
	check("a sync that could not be prepared", newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true, stateDir: &empty}), false)

	p := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	p.s.panics = true
	check("a panicking session", p, false)
}

// covers: MA-913, R37, S64
// Two Run now clicks at once (a double click, or two pages) on one encrypted archive
// (gate-2 adversarial pass after AS8): each takes a lease and opens a window; the one
// that takes the job slot is the run a Lock must reach, whichever click came first.
// Here the second click takes the slot while the first sits between its window and
// the slot; the first is refused, its lease destroyed and its window gone; a Lock
// while the run that won waits cancels it — its Stop ended with the Lock's cause, the
// reply says it was cancelled and Thunderbird left open, its key destroyed once it
// returns, recorded cancelled, nothing read. A Lock between a click's window and the
// slot (no run has begun) cancels it as well: no session starts and the record is
// left as it was. Positive twin first: one click's window is open while it waits.
func TestALockReachesTheRunNowThatTookTheSlot(t *testing.T) {
	g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	g.start()
	g.at(presync.PhaseWaiting)
	if !g.windowOpen() {
		t.Fatal("twin: no Lock window while an encrypted Run now waits")
	}
	g.s.release <- struct{}{}
	g.done()

	g = newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	var second map[string]any
	clicked := false
	pauseRunNowAt(t, "window", func() {
		if !clicked {
			clicked = true
			second = callAction(t, g.d, g.d.runBackup, nil) // the other click takes the slot first
		}
	})
	first := callAction(t, g.d, g.d.runBackup, nil)
	if second["ok"] != true || first["ok"] == true || !strings.Contains(str(first["error"]), "already running") {
		t.Fatalf("fixture: the second click did not take the slot first: first %v, second %v", first, second)
	}
	g.mu.Lock()
	leases := append([]*keyring.Unlocked(nil), g.leases...)
	g.mu.Unlock()
	if len(leases) != 2 {
		t.Fatalf("fixture: %d leases, want 2", len(leases))
	}
	if _, err := leases[0].Lease(); !errors.Is(err, keyring.ErrLocked) {
		t.Errorf("the refused click's lease outlived it: %v", err)
	}
	g.at(presync.PhaseWaiting)
	if !g.windowOpen() {
		t.Error("no Lock window for the Run now that took the slot")
	}
	reply := g.lock()
	want := lockCancelledText + " Thunderbird, which it started out of sight, was left open — quit it from the Dock before the next Run now."
	if str(reply["note"]) != want {
		t.Errorf("the Lock reply %q, want %q", reply["note"], want)
	}
	g.done()
	_, cause, stopped := g.s.last()
	if !stopped || !errors.Is(cause, errLockedDuringSync) {
		t.Errorf("a Lock delivered cause %v, Stop ended %v; want errLockedDuringSync and the Stop ended", cause, stopped)
	}
	if k := g.lastKey(); k == nil || keyUsable(k) {
		t.Error("the run's key outlived the run after a Lock")
	}
	if _, err := leases[1].Lease(); !errors.Is(err, keyring.ErrLocked) {
		t.Errorf("the Lock-cancelled run's lease outlived it: %v", err)
	}
	if g.status() != state.RunCancelled || g.windowOpen() {
		t.Errorf("after a Lock: record %s, window open %v; want cancelled, closed", g.status(), g.windowOpen())
	}
	if !g.nothingWasRead() {
		t.Error("the Lock-cancelled run read mail")
	}

	// A Lock between a click's window and the job slot.
	g = newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	before := g.status()
	var early map[string]any
	pauseRunNowAt(t, "window", func() { early = g.lock() })
	g.start()
	g.done()
	runNowPause = func(string) {}
	if str(early["note"]) != lockCancelledText {
		t.Errorf("a Lock before the slot: the reply %q, want %q", early["note"], lockCancelledText)
	}
	if g.s.calls() != 0 || g.status() != before || g.windowOpen() {
		t.Errorf("a Lock before the slot: %d sessions, record %s (want %s), window open %v", g.s.calls(), g.status(), before, g.windowOpen())
	}
	if _, err := g.lastLease().Lease(); !errors.Is(err, keyring.ErrLocked) {
		t.Errorf("a Lock before the slot: the lease outlived the run: %v", err)
	}
	if !g.nothingWasRead() {
		t.Error("a Lock before the slot: the run read mail")
	}
}

// covers: MA-904, R37, S65
// Nothing about the sync is written beside an encrypted archive, end to end through
// the Desktop's own encrypted Run now (design-presync-attended §4.4, C-16; the part
// MA-904's row leaves to slice AS8; gate-2 pass after AS8). Positive twin first: a
// plaintext Run now that synced writes presync to its record. Then, on an encrypted
// archive whose Thunderbird profile and mail folder carry markers in their names, a
// synced Run now and one cancelled by a Lock while it waits: after each, the last-run
// file's keys and every history entry's keys stay within the A18 allow-list, a scan of
// the whole archive folder finds no outcome code or long name, no "presync",
// "Thunderbird", "synced" or "wait_ended" and neither marker, and there is no
// sync-streak file and no mailarchive.log.
func TestAnEncryptedRunNowLeavesNothingOfTheSyncBesideIt(t *testing.T) {
	pg := newRunNowRig(t, "darwin", true)
	pg.start()
	pg.at(presync.PhaseWaiting)
	pg.s.release <- struct{}{}
	pg.done()
	if p, ok := rawLastRun(t, pg.out)["presync"].(map[string]any); !ok || p["name"] != "ok" {
		t.Fatalf("twin: the plaintext Run now's record has no presync named ok: %v", rawLastRun(t, pg.out))
	}

	g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	profMarker, folderMarker := "zqprofmarkerkvx", "zqfoldmarkerkvx"
	profile := filepath.Join(t.TempDir(), profMarker+".default")
	store := filepath.Join(profile, "Mail", folderMarker)
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(profile, "prefs.js"), []byte("// prefs\n"), 0o644)
	os.WriteFile(filepath.Join(store, "Inbox"), []byte(laterMbox), 0o644)
	j, err := job.Read(g.jp)
	if err != nil {
		t.Fatal(err)
	}
	j.Inputs = []string{store}
	if err := job.Write(g.jp, j); err != nil {
		t.Fatal(err)
	}

	needles := []string{"presync", "thunderbird", "Thunderbird", "synced", "wait_ended", profMarker, folderMarker}
	for _, o := range presync.Outcomes() {
		if o.Code != "" {
			needles = append(needles, o.Code)
		}
		// Short or status-word names ("ok", "cancelled") are the record's own status
		// words: the key-set check covers them.
		if len(o.Name) >= 6 && o.Name != presync.OutcomeCancelled.Name {
			needles = append(needles, o.Name)
		}
	}
	allowRecord := map[string]bool{"version": true, "status": true, "started": true, "finished": true, "error_kind": true, "program": true, "encrypted": true}
	allowEntry := map[string]bool{"when": true, "status": true, "errorKind": true, "encrypted": true}
	check := func(label string) {
		t.Helper()
		lr := rawLastRun(t, g.out)
		assure.Reached(t, len(lr), label+": last-run keys")
		for k := range lr {
			if !allowRecord[k] {
				t.Errorf("%s: the encrypted last-run carries %q, outside the A18 allow-list", label, k)
			}
		}
		b, err := os.ReadFile(filepath.Join(g.out, state.RunHistoryName))
		if err != nil {
			t.Fatal(err)
		}
		var h []map[string]any
		if err := json.Unmarshal(b, &h); err != nil {
			t.Fatalf("%s: the history is not JSON: %v", label, err)
		}
		assure.Reached(t, len(h), label+": history entries")
		for i, e := range h {
			for k := range e {
				if !allowEntry[k] {
					t.Errorf("%s: history entry %d carries %q, outside the A18 allow-list", label, i, k)
				}
			}
		}
		for _, n := range needles {
			hits, err := leak.ScanTree(g.out, leak.Markers{leak.SourcePath: n})
			if err != nil {
				t.Fatal(err)
			}
			for _, hit := range hits {
				t.Errorf("%s: the encrypted archive names %q: %s", label, n, hit)
			}
		}
		for _, f := range []string{"mailarchive.log", state.SyncStreakName} {
			if _, err := os.Stat(filepath.Join(g.out, f)); !os.IsNotExist(err) {
				t.Errorf("%s: the encrypted archive holds %s (%v)", label, f, err)
			}
		}
	}

	g.start()
	g.at(presync.PhaseWaiting)
	g.s.release <- struct{}{}
	if m := g.done(); g.status() != state.RunOK || !strings.Contains(str(m["note"]), "Thunderbird synced") {
		t.Fatalf("fixture: the encrypted Run now did not sync and back up: %v, record %s", m, g.status())
	}
	check("synced")

	g.start()
	g.at(presync.PhaseWaiting)
	g.lock()
	g.done()
	if g.status() != state.RunCancelled {
		t.Fatalf("fixture: the Lock did not cancel the Run now: record %s", g.status())
	}
	check("Lock-cancelled")
}

// rawLastRun is the last-run file on disk as a key map.
func rawLastRun(t *testing.T, out string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, state.LastRunName))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("the last-run file is not JSON: %v\n%s", err, b)
	}
	return m
}

// covers: MA-913, R37
// The Lock reply's words (§9, C-64, OQ-2, OQ-8): nothing about Thunderbird before the
// sync began or for a Run now with no sync; "may still be starting" while it starts;
// "left open" once its Thunderbird was proven. On Windows the taskbar and the
// notification area, never the Dock; and the window called minimized only when the
// sync ended saying so — while it waits, or when the minimize never succeeded, "its
// window may still be on screen".
func TestTheLockReplyWords(t *testing.T) {
	type c struct {
		goos, phase string
		ended       bool
		left        presync.Left
		minimized   bool
		want        string
	}
	for _, x := range []c{
		{"darwin", "", false, "", false, lockCancelledText},
		{"darwin", presync.PhaseStarting, false, "", false, lockCancelledText + " Thunderbird may still be starting; if it appears in the Dock, quit it."},
		{"darwin", presync.PhaseWaiting, false, "", false, lockCancelledText + " Thunderbird, which it started out of sight, was left open — quit it from the Dock before the next Run now."},
		{"darwin", "", true, presync.LeftOpenByRule, false, lockCancelledText + " Thunderbird, which it started out of sight, was left open — quit it from the Dock before the next Run now."},
		{"darwin", "", true, presync.LeftNone, false, lockCancelledText},
		{"windows", presync.PhaseStarting, false, "", false, lockCancelledText + " Thunderbird may still be starting; if it appears in the taskbar or the notification area, quit it."},
		{"windows", presync.PhaseWaiting, false, "", false, lockCancelledText + " Thunderbird, which it started, was left open; its window may still be on screen — quit it from the taskbar or the notification area before the next Run now."},
		{"windows", "", true, presync.LeftOpenByRule, false, lockCancelledText + " Thunderbird, which it started, was left open; its window may still be on screen — quit it from the taskbar or the notification area before the next Run now."},
		{"windows", "", true, presync.LeftOpenByRule, true, lockCancelledText + " Thunderbird, which it started minimized to the taskbar, was left open — quit it from the taskbar or the notification area before the next Run now."},
		{"windows", "", true, presync.LeftMayBeStarting, false, lockCancelledText + " Thunderbird may still be starting; if it appears in the taskbar or the notification area, quit it."},
	} {
		got := lockCancelNote(x.goos, x.phase, x.ended, string(x.left), x.minimized)
		if got != x.want {
			t.Errorf("%s %q ended %v %q min %v:\n got %q\nwant %q", x.goos, x.phase, x.ended, x.left, x.minimized, got, x.want)
		}
		if x.goos == "windows" && (strings.Contains(got, "Dock") || strings.Contains(got, "out of sight")) {
			t.Errorf("a Windows Lock reply names the Dock: %q", got)
		}
	}
	for _, s := range []string{lockCancelledText, lockReadingText} {
		if strings.Contains(s, "mailarchive ") || strings.Contains(s, "closed") || strings.Contains(s, "already gone") {
			t.Errorf("a Lock reply names a command, a close or a key already gone: %q", s)
		}
	}
	if !strings.Contains(lockReadingText, "finishes with its own key, then the key is cleared") || !strings.Contains(lockReadingText, "Click Cancel to stop it now") {
		t.Errorf("the reading reply: %q", lockReadingText)
	}
	// Friction review (§14 gate 3): the reply says what the Lock did in plain words —
	// nothing new can open the archive — never the unclear "Locked for new use".
	if !strings.HasPrefix(lockReadingText, "Locked: nothing new can open the archive.") || strings.Contains(lockReadingText, "for new use") {
		t.Errorf("the reading reply's first sentence: %q", lockReadingText)
	}
}

// covers: MA-911, X16, S64
// The encrypted Settings card's sync-only box (design-presync-attended §8.1, §8.3,
// §4.7; C-24, C-25, C-26, C-27, C-49, C-52). Positive twin first: on macOS, for an
// unlocked encrypted archive whose Run now job is made from a Thunderbird profile
// here, the card shows "Sync Thunderbird before each Run now", its line (the sync runs
// at Run now, the archive stays unlocked for that backup while it waits, Lock cancels
// it until it reads mail), the encrypted SyncNotice (never-close), no Mac note, and its
// own Save through busy(). Windows names the taskbar. Absent on Linux, for an Outlook
// job, and for a job not made from Thunderbird with the sync off; shown for one with
// the sync on (never hidden while on); for an unreadable job a one-line reason instead.
// Where Run now does not start Thunderbird yet, it shows only while on, with a reason.
// After a synced Run now the card and the health view show the setting and the last
// result while the archive stays unlocked here; a Lock clears the result.
func TestTheEncryptedSettingsSyncBox(t *testing.T) {
	g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: false})
	page, _ := getPage(t, DashboardHandler(g.d.cfg))
	if !strings.Contains(page, `id="encSyncBox"`) || !shows(page, encSyncLabel) || !shows(page, encSyncLine) ||
		!shows(page, encSyncNotice("darwin")) || strings.Contains(page, `id="encSyncPresync" checked`) {
		t.Fatal("twin: the encrypted card has no sync-only box for a job made from Thunderbird")
	}
	if strings.Contains(page, `id="schedMacNote"`) || strings.Contains(page, `id="schedFields"`) {
		t.Error("the encrypted card shows a Mac note or the schedule fields")
	}
	if !strings.Contains(page, `id="encSyncSave"`) ||
		!regexp.MustCompile(`esb\.addEventListener\('click',async\(\)=>\{\s*const n=\$\('encSyncNote'\),bx=\$\('encSyncPresync'\),done=busy\(esb,'Saving…'`).MatchString(dashboardJS) ||
		!strings.Contains(dashboardJS, `fetch('api/runnow-sync',{method:'POST'`) {
		t.Error("the box's own Save does not post to api/runnow-sync through busy()")
	}
	for _, want := range []string{"while you are here", "the archive stays unlocked for that backup", "Until the backup starts reading mail, Lock cancels it; after that, Cancel stops it."} {
		if !strings.Contains(encSyncLine, want) {
			t.Errorf("the box's line lacks %q", want)
		}
	}
	shown := func(goos string, startAllowed bool) bool {
		r := presync.OverrideTablesForTest(nil, map[string]bool{goos: startAllowed})
		defer r()
		return encSyncCard(g.out, goos, "").SyncOnly
	}
	if !shown("windows", true) || shown("linux", true) {
		t.Error("the box is not shown on Windows, or is shown on Linux")
	}
	if strings.Contains(encSyncNotice("windows"), "Dock") || !strings.Contains(encSyncNotice("windows"), "minimized to the taskbar") {
		t.Errorf("the Windows notice: %q", encSyncNotice("windows"))
	}
	if shown("darwin", false) {
		t.Error("the box is offered to switch on where Run now does not start Thunderbird")
	}

	j, _ := job.Read(g.jp)
	write := func(j job.Job) {
		if err := job.Write(g.jp, j); err != nil {
			t.Fatal(err)
		}
	}
	o := j
	o.Outlook = true
	write(o)
	if shown("darwin", true) {
		t.Error("the box is shown for an Outlook job")
	}
	n := j
	n.Inputs = []string{t.TempDir()}
	write(n)
	if shown("darwin", true) {
		t.Error("the box is shown for a job not made from Thunderbird, the sync off")
	}
	n.ThunderbirdSync = &job.ThunderbirdSync{}
	write(n)
	if !shown("darwin", true) {
		t.Error("the box is hidden while the sync is on")
	}
	r := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": false})
	if c := encSyncCard(g.out, "darwin", ""); !c.SyncOnly || c.SyncReason != encSyncStartOffLine {
		t.Errorf("the start not allowed, the sync on: %+v", c)
	}
	r()
	os.WriteFile(g.jp, []byte("{not a job"), 0o600)
	if c := encSyncCard(g.out, "darwin", ""); c.SyncOnly || !strings.Contains(c.SyncUnreadable, "can't be read") {
		t.Errorf("an unreadable job: %+v", c)
	}
	page, _ = getPage(t, DashboardHandler(g.d.cfg))
	if strings.Contains(page, `id="encSyncBox"`) || !strings.Contains(page, `id="encSyncUnreadable"`) {
		t.Error("an unreadable job: the card shows the box, or no reason")
	}

	// The setting and the last result, while unlocked.
	j.ThunderbirdSync = &job.ThunderbirdSync{}
	write(j)
	g.start()
	g.at(presync.PhaseWaiting)
	g.s.release <- struct{}{}
	g.done()
	page, _ = getPage(t, DashboardHandler(g.d.cfg))
	if !strings.Contains(page, `id="encSyncPresync" checked`) || !shows(page, "Last Run now here: Thunderbird synced first.") {
		t.Error("the card does not show the setting on and the last Run now's sync")
	}
	if !strings.Contains(g.health(), encSyncSettingLine("Thunderbird synced first")) {
		t.Errorf("the health view: %q", g.health())
	}
	g.lock()
	page, _ = getPage(t, DashboardHandler(g.d.cfg))
	if shows(page, "Last Run now here") || strings.Contains(g.health(), "last Run now here") || !strings.Contains(g.health(), "Thunderbird sync: on (Run now only).") {
		t.Errorf("after a Lock the last result is still shown, or the setting is not: %q", g.health())
	}
}

func (g *encRig) health() string {
	rec := httptest.NewRecorder()
	g.d.healthAction(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var m struct {
		Summary []string `json:"summary"`
	}
	json.Unmarshal(rec.Body.Bytes(), &m)
	return strings.Join(m.Summary, "\n")
}

// covers: MA-912, R30, R37, S64
// api/runnow-sync (design-presync-attended §8.2, C-18, C-26): positive twin first,
// switching on edits exactly the job Run now would run — under a non-default name too
// — changing only thunderbird_sync and writing no profile, and switching off removes
// it; it works while the archive is locked (no key needed). Each refusal leaves the
// job file, the archive, the scheduler and the settings unchanged and says "Nothing
// was changed.": no drawnFor, a drawnFor naming another archive, no presync (or not a
// boolean), a plaintext archive, Linux, a paused archive, a task running, no Run now
// job here, a job that can't be read, switching on where Run now does not start
// Thunderbird, and switching on for a job not made from Thunderbird. The route is
// POST-only and guarded.
func TestRunNowSyncRoute(t *testing.T) {
	g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: false})
	installs := 0
	g.d.cfg.InstallBackup = func(schedule.Spec) error { installs++; return nil }
	custom := filepath.Join(filepath.Dir(g.jp), "mailarchive-my-own-name.json")
	if err := os.Rename(g.jp, custom); err != nil {
		t.Fatal(err)
	}
	before, _ := job.Read(custom)
	g.lock()
	on := callAction(t, g.d, g.d.runNowSyncAction, map[string]any{"drawnFor": g.out, "presync": true})
	after, err := job.Read(custom)
	if on["ok"] != true || str(on["note"]) != "Saved. Run now will sync Thunderbird first." || err != nil || after.ThunderbirdSync == nil || after.ThunderbirdSync.Profile != "" {
		t.Fatalf("twin: switching on: %v, job %+v (%v)", on, after, err)
	}
	if _, err := os.Stat(g.jp); !os.IsNotExist(err) {
		t.Error("switching on wrote the default-named job file")
	}
	after.ThunderbirdSync = nil
	if b1, b2 := mustJSON(before), mustJSON(after); b1 != b2 {
		t.Errorf("switching on changed more than the sync:\n%s\n%s", b1, b2)
	}
	off := callAction(t, g.d, g.d.runNowSyncAction, map[string]any{"drawnFor": g.out, "presync": false})
	if j, _ := job.Read(custom); off["ok"] != true || str(off["note"]) != "Saved. Run now will not sync Thunderbird." || j.ThunderbirdSync != nil {
		t.Fatalf("switching off: %v", off)
	}
	raw, _ := os.ReadFile(custom)
	if strings.Contains(string(raw), "profile") {
		t.Errorf("the job file names a profile: %s", raw)
	}

	refused := func(label string, body any, names ...string) {
		t.Helper()
		snap := encSnapshot(t, g)
		m := callAction(t, g.d, g.d.runNowSyncAction, body)
		assure.Refused(t, http.StatusOK, str(m["error"]), assure.Code(http.StatusOK), assure.Names(append(names, "Nothing was changed")...),
			assure.NoSideEffect(func() bool { return m["ok"] != true && encSnapshot(t, g) == snap && installs == 0 }))
	}
	refused("no drawnFor", map[string]any{"presync": true}, "reload")
	refused("another archive", map[string]any{"drawnFor": t.TempDir(), "presync": true}, "reload")
	refused("no presync", map[string]any{"drawnFor": g.out}, "Say whether")
	refused("presync not a boolean", map[string]any{"drawnFor": g.out, "presync": "yes"}, "Say whether")
	g.d.cfg.GOOS = "linux"
	refused("Linux", map[string]any{"drawnFor": g.out, "presync": true}, "macOS and Windows")
	g.d.cfg.GOOS = "darwin"
	seal.Seal(g.out, "")
	refused("paused", map[string]any{"drawnFor": g.out, "presync": true}, "paused")
	seal.Unseal(g.out)
	g.d.cap.mu.Lock()
	g.d.cap.running = true
	g.d.cap.mu.Unlock()
	refused("a task running", map[string]any{"drawnFor": g.out, "presync": true}, "running")
	g.d.cap.mu.Lock()
	g.d.cap.running = false
	g.d.cap.mu.Unlock()
	r := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": false})
	refused("Run now does not start Thunderbird", map[string]any{"drawnFor": g.out, "presync": true}, "does not start Thunderbird")
	r()
	j, _ := job.Read(custom)
	plain := j
	plain.Inputs = []string{t.TempDir()}
	job.Write(custom, plain)
	refused("not made from Thunderbird", map[string]any{"drawnFor": g.out, "presync": true}, "wasn't made from a Thunderbird profile")
	job.Write(custom, j)
	os.Remove(custom)
	os.WriteFile(g.jp, []byte("{not a job"), 0o600)
	refused("an unreadable job", map[string]any{"drawnFor": g.out, "presync": true}, "can't be read")
	elsewhere := j
	elsewhere.Out = t.TempDir()
	job.Write(g.jp, elsewhere)
	refused("a job naming another archive", map[string]any{"drawnFor": g.out, "presync": true}, "another archive folder")
	os.Remove(g.jp)
	refused("no Run now job", map[string]any{"drawnFor": g.out, "presync": true}, "No backup is set up")

	// A plaintext archive: use Save settings.
	pg := newRunNowRig(t, "darwin", true)
	m := callAction(t, pg.d, pg.d.runNowSyncAction, map[string]any{"drawnFor": pg.out, "presync": true})
	jb, _ := os.ReadFile(pg.jp)
	assure.Refused(t, http.StatusOK, str(m["error"]), assure.Code(http.StatusOK), assure.Names("not encrypted", "Save settings", "Nothing was changed"),
		assure.NoSideEffect(func() bool { b, _ := os.ReadFile(pg.jp); return m["ok"] != true && string(b) == string(jb) }))

	// POST-only and guarded.
	rec := httptest.NewRecorder()
	g.d.api(g.d.runNowSyncAction)(rec, httptest.NewRequest(http.MethodGet, "/api/runnow-sync", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a GET: %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/runnow-sync", strings.NewReader(`{"presync":true}`))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	g.d.api(g.d.runNowSyncAction)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a post with no CSRF token: %d, want 403", rec.Code)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// encSnapshot is the job folder's, the archive's and the settings file's contents.
func encSnapshot(t *testing.T, g *encRig) string {
	t.Helper()
	var b strings.Builder
	for _, root := range []string{filepath.Dir(g.jp), g.out} {
		filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() {
				data, _ := os.ReadFile(p)
				b.WriteString(p + "\x00" + string(data) + "\x00")
			}
			return nil
		})
	}
	s, _ := os.ReadFile(g.d.cfg.SettingsPath)
	b.Write(s)
	return b.String()
}

// covers: MA-915, X16, X11
// The encrypted texts AS8 adds (design-presync-attended §7, C-25, C-49, C-52, C-61,
// OQ-2, OQ-3, OQ-8), never-close only: Run now's encrypted tooltip — with the sync off
// (or on Linux) today's words and the Lock sentence; with it on, the plaintext sync
// text, that the archive stays unlocked for the backup while it waits, and the Lock
// sentence; where the start is not allowed, a variant naming no scheduled backup —
// rendered on the card for the job Run now would run, also under a non-default name;
// the encrypted SyncNotice carries every X16 disclosure that applies to a Run now that
// never closes Thunderbird and no scheduled-backup sentence. Windows says the taskbar
// and the notification area, never the Dock or "out of sight"; macOS never the
// taskbar; no text says Thunderbird is closed, names on-exit actions, offers "Keep
// Thunderbird open" or names a command.
func TestEncryptedRunNowTexts(t *testing.T) {
	syncOff := "Back up this encrypted archive now, while it is unlocked here. Until the backup starts reading mail, Lock cancels it; after that, Cancel stops it."
	var all []struct{ goos, s string }
	for _, goos := range []string{"darwin", "windows", "linux"} {
		if encRunNowTitle(goos, false) != syncOff {
			t.Errorf("%s, sync off: %q", goos, encRunNowTitle(goos, false))
		}
	}
	r := presync.OverrideTablesForTest(nil, map[string]bool{"linux": true})
	if encRunNowTitle("linux", true) != syncOff {
		t.Error("Linux shows a sync sentence")
	}
	r()
	for _, goos := range []string{"darwin", "windows"} {
		r := presync.OverrideTablesForTest(nil, map[string]bool{goos: true})
		on, plain := encRunNowTitle(goos, true), runNowTitle(goos, true)
		r()
		if !strings.HasPrefix(on, plain+" ") {
			t.Errorf("%s: not the plaintext sync text first: %q", goos, on)
		}
		if !strings.HasSuffix(on, " While it waits for Thunderbird, the archive stays unlocked for this backup. "+encLockSentence) {
			t.Errorf("%s, sync on: %q", goos, on)
		}
		r = presync.OverrideTablesForTest(nil, map[string]bool{goos: false})
		startOff := encRunNowTitle(goos, true)
		r()
		if startOff != encRunNowStartOff || strings.Contains(startOff, "cheduled") || !strings.HasSuffix(startOff, encLockSentence) {
			t.Errorf("%s, start not allowed: %q", goos, startOff)
		}
		notice := encSyncNotice(goos)
		for _, want := range []string{"checks all your accounts", "sends anything waiting in your Outbox", "runs your message filters", "may show notifications",
			"A Thunderbird you already have open is left alone — never started or closed.", "only the one holding this mailbox is synced", "(up to 20 minutes, often about 13)", "leaves it open for you to quit"} {
			if !strings.Contains(notice, want) {
				t.Errorf("%s: the encrypted SyncNotice lacks %q", goos, want)
			}
		}
		if strings.Contains(notice, "cheduled") {
			t.Errorf("%s: the encrypted SyncNotice names a scheduled backup: %q", goos, notice)
		}
		all = append(all, struct{ goos, s string }{goos, on}, struct{ goos, s string }{goos, startOff}, struct{ goos, s string }{goos, notice})
	}
	all = append(all, struct{ goos, s string }{"", encSyncLine}, struct{ goos, s string }{"", encSyncStartOffLine}, struct{ goos, s string }{"", encSyncLabel})
	for _, x := range all {
		if x.goos == "windows" && (strings.Contains(x.s, "Dock") || strings.Contains(x.s, "out of sight") || (strings.Contains(x.s, "quit") && !strings.Contains(x.s, "the taskbar or the notification area"))) {
			t.Errorf("a Windows text: %q", x.s)
		}
		if x.goos == "darwin" && strings.Contains(x.s, "taskbar") {
			t.Errorf("a macOS text names the taskbar: %q", x.s)
		}
		for _, forbid := range []string{"closes it", "was closed", "is closed", "Keep Thunderbird open", "on-exit", "asks before", "being written", "mailarchive ", "`"} {
			if strings.Contains(x.s, forbid) {
				t.Errorf("a never-close encrypted text says %q: %q", forbid, x.s)
			}
		}
	}

	// Rendered on the card, for the job Run now would run under a non-default name.
	g := newEncRig(t, encRigOpts{goos: "darwin", startAllowed: true, sync: true})
	if page, _ := getPage(t, DashboardHandler(g.d.cfg)); !shows(page, encRunNowTitle("darwin", true)) {
		t.Error("the card's encrypted Run now tooltip is not the sync-on text")
	}
	custom := filepath.Join(filepath.Dir(g.jp), "mailarchive-my-own-name.json")
	j, _ := job.Read(g.jp)
	j.ThunderbirdSync = nil
	job.Write(g.jp, j)
	if err := os.Rename(g.jp, custom); err != nil {
		t.Fatal(err)
	}
	if page, _ := getPage(t, DashboardHandler(g.d.cfg)); !shows(page, syncOff) {
		t.Error("the card's encrypted Run now tooltip, sync off under a non-default name, is not the Lock text")
	}
}
