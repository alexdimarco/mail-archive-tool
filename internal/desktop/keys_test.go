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
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/uninstall"
)

// The Desktop's unlock, lock and reader of an encrypted archive (design-encrypted-
// archives-phase3 §3.2, §7.1, K4, O3; the P3-3 plan, 3c; MA-805).

const keysPassword = "desktop test passphrase"

const keysMbox = "From - Mon Jan 01 00:00:00 2024\r\n" +
	"From: Bob <bob@example.com>\r\nSubject: Hello HTML\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n" +
	"Message-ID: <abc@example.com>\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
	"<p>Hello <b>world</b> invoice</p>\r\n"

// backedUpEncrypted makes an encrypted archive holding one message, with its index.
func backedUpEncrypted(t *testing.T) string {
	t.Helper()
	// Its own record of encrypted archives, so no other test sees it recorded.
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
	backUpInto(t, out, u)
	return out
}

// backUpInto runs a keyed backup of one message into the archive u opened for a write.
func backUpInto(t *testing.T, out string, u *keyring.Unlocked) {
	t.Helper()
	box := filepath.Join(t.TempDir(), "Inbox")
	os.WriteFile(box, []byte(keysMbox), 0o600)
	opts := app.Options{Inputs: []string{box}, Out: out, Index: true, KeepRaw: true}
	done, err := keyring.Apply(&opts, u)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if _, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
}

// keysEnv is a Desktop whose current archive is an encrypted one, with its registry,
// its own secret store, and its reader.
type keysEnv struct {
	h      http.Handler
	reader http.Handler
	csrf   string
	cfg    Config
	out    string
}

func newKeysEnv(t *testing.T, out string) *keysEnv {
	t.Helper()
	dir := t.TempDir()
	settings := filepath.Join(dir, "desktop-settings.json")
	if err := SaveSettings(settings, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	store := graphconfig.NewFileSecretStore(t.TempDir())
	cfg := Config{SettingsPath: settings, IndexPath: filepath.Join(dir, "known-archives.json"), Store: store, Keys: newArchiveKeys(store, readerLockedNote)}
	sess := localweb.NewSession()
	h := dashboardHandler(cfg, loopback(8097), sess, nil)
	page := sessionPage(t, h)
	m := dashCSRFRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no CSRF token on the page")
	}
	rm := ReaderHandler(cfg)
	t.Cleanup(func() { cfg.Keys.lockAll(true); rm.(*readerMux).Close() })
	return &keysEnv{h: h, reader: rm, csrf: m[1], cfg: cfg, out: out}
}

func (e *keysEnv) post(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	s, _ := localweb.Guarded(e.h)
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, s.Session.Prefix()+path, strings.NewReader(string(b)))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", e.csrf)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie(t, e.h, s.Session, req.Host))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func (e *keysEnv) read(path string) (int, string) {
	rec := httptest.NewRecorder()
	e.reader.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

// covers: MA-805, R37, X9
// Unlock on the Encryption card, never in the reader (§7.1): locked, the page offers
// the password form and the reader answers every page with the locked page, naming
// where to unlock; a wrong password is refused, not echoed, nothing opened or
// recorded; the right one opens it — the reader then finds its message — and records
// nothing on this computer (§3.3); Lock closes it at once, the reader locked again;
// a POST without the page's token is refused.
func TestTheEncryptionCardUnlocksAndLocks(t *testing.T) {
	out := backedUpEncrypted(t)
	t.Cleanup(encrecord.SetDirForTest(t.TempDir())) // another computer: the folder is not recorded here
	e := newKeysEnv(t, out)

	page := sessionPage(t, e.h)
	if !strings.Contains(page, `id="sec-encryption"`) || !strings.Contains(page, `id="encUnlockForm"`) || !strings.Contains(page, `src="encryption.js"`) {
		t.Fatal("a locked encrypted archive's page offers the Encryption card's unlock")
	}
	if code, body := e.read("/"); code != http.StatusServiceUnavailable || !strings.Contains(body, "Encryption card") {
		t.Fatalf("the reader of a locked archive: %d\n%s", code, body)
	}

	code, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": "not the password at all"})
	assure.Refused(t, code, toText(m), assure.Code(http.StatusOK), assure.Names("does not open this archive"), assure.Forbid("not the password at all"),
		assure.NoSideEffect(func() bool { return e.cfg.Keys.get(out) == nil }))

	code, m = e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if code != http.StatusOK || m["ok"] != true || e.cfg.Keys.get(out) == nil {
		t.Fatalf("unlock: %d %v", code, m)
	}
	if strings.Contains(toText(m), keysPassword) {
		t.Fatal("the reply echoes the password")
	}
	if rec, _ := encrecord.Load(); len(rec.Archives) != 0 {
		t.Fatal("a read unlock recorded the folder")
	}
	if code, body := e.read("/api/search?q=hello"); code != http.StatusOK || !strings.Contains(body, "Hello HTML") {
		t.Fatalf("the reader after the unlock: %d\n%s", code, body)
	}
	if page := sessionPage(t, e.h); !strings.Contains(page, `id="encLock"`) {
		t.Fatal("an unlocked archive's card offers Lock")
	}

	if code, m := e.post(t, "api/encryption/lock", nil); code != http.StatusOK || m["ok"] != true {
		t.Fatalf("lock: %d %v", code, m)
	}
	code, body := e.read("/api/search?q=hello")
	if strings.Contains(body, "Hello HTML") || e.cfg.Keys.get(out) != nil {
		t.Fatalf("the reader after Lock: %d\n%s", code, body)
	}

	s, _ := localweb.Guarded(e.h)
	req := httptest.NewRequest(http.MethodPost, s.Session.Prefix()+"api/encryption/unlock", strings.NewReader(`{"secret":"`+keysPassword+`"}`))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.AddCookie(sessionCookie(t, e.h, s.Session, req.Host))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.Names("refused"), assure.Forbid(keysPassword),
		assure.NoSideEffect(func() bool { return e.cfg.Keys.get(out) == nil }))
}

// covers: MA-805, R37
// The key this computer remembers (O3): unlocked with Remember ticked, the archive is
// recorded and its key remembered; after an idle lock its reader opens it again by
// itself, with no unlock asked; after the person's own Lock it does not, until they
// unlock it again.
func TestARememberedArchiveOpensByItselfUnlessLockedHere(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	if code, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true}); code != http.StatusOK || m["ok"] != true {
		t.Fatalf("unlock with remember: %d %v", code, m)
	}
	if !keyring.HasRemembered(out, e.cfg.Store) {
		t.Fatal("the key is remembered")
	}
	e.cfg.Keys.reap(time.Now().Add(keyIdle + time.Minute))
	if e.cfg.Keys.get(out) != nil {
		t.Fatal("an idle Desktop locks")
	}
	if code, body := e.read("/api/search?q=hello"); code != http.StatusOK || !strings.Contains(body, "Hello HTML") {
		t.Fatalf("a remembered archive opens by itself: %d %s", code, body)
	}
	e.post(t, "api/encryption/lock", nil)
	_, body := e.read("/api/search?q=hello")
	assure.Reached(t, !strings.Contains(body, "Hello HTML") && e.cfg.Keys.get(out) == nil, "locked here, it stays locked")
}

// covers: MA-805, R37
// The idle lock (K4): a person's use of the Desktop holds it off, the page's own
// polling does not; at 29 idle minutes the archive is open, past 30 it is locked. A
// switch of the current archive locks the one left. Shut down locks every archive and
// refuses every unlock after it.
func TestTheDesktopLocksWhenIdleSwitchedOrShutDown(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	now := t0
	e.cfg.Keys.now = func() time.Time { return now }
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	now = t0.Add(20 * time.Minute)
	sessionRequest(t, e.h, http.MethodGet, "api/activity", "") // a poll: not a person
	e.cfg.Keys.reap(t0.Add(29 * time.Minute))
	if e.cfg.Keys.get(out) == nil {
		t.Fatal("open at 29 idle minutes")
	}
	e.cfg.Keys.reap(t0.Add(31 * time.Minute))
	if e.cfg.Keys.get(out) != nil {
		t.Fatal("a poll held off the idle lock")
	}
	now = t0.Add(40 * time.Minute)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	now = t0.Add(60 * time.Minute)
	sessionPage(t, e.h) // a person loads the page
	e.cfg.Keys.reap(t0.Add(85 * time.Minute))
	if e.cfg.Keys.get(out) == nil {
		t.Fatal("a person's use held off the idle lock")
	}

	other := archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"})
	e.post(t, "api/settings", map[string]any{"out": other})
	if e.cfg.Keys.get(out) != nil {
		t.Fatal("a switch locks the archive left")
	}
	SaveSettings(e.cfg.SettingsPath, Settings{Out: out})
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	e.cfg.Keys.lockAll(true)
	_, err := e.cfg.Keys.unlock(out, keysPassword, keyring.OpenOptions{})
	assure.Reached(t, e.cfg.Keys.get(out) == nil && errors.Is(err, errKeysClosed), "Shut down locks and refuses")
}

// covers: MA-805, R37
// Open an archive folder… (§7.1): the folder picked is judged before anything else —
// an archive at its own folder, plaintext or encrypted, is answered for the switch; a
// folder inside an encrypted archive names the archive's own folder; a folder that is
// no archive is told so; a cancelled picker is nothing. The button is on the first
// card of a Desktop with no archive yet, in the words the note will quote.
func TestOpenAnArchiveFolder(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	enc := recordedArchive(t, t.TempDir(), "Enc")
	plain := archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"})
	inside := filepath.Join(enc, "units")
	os.MkdirAll(inside, 0o700)
	empty := t.TempDir()

	d := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	var picked string
	d.pick = func(context.Context, string) (string, error) { return picked, nil }
	open := func() map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/api/open-archive", strings.NewReader("{}"))
		req.Host = "127.0.0.1:8097"
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("X-CSRF-Token", d.csrf)
		rec := httptest.NewRecorder()
		d.api(d.openArchiveAction)(rec, req)
		var m map[string]any
		json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}
	for _, p := range []string{enc, plain} {
		picked = p
		if m := open(); m["ok"] != true || m["path"] != p {
			t.Fatalf("an archive at its own folder: %v", m)
		}
	}
	picked = inside
	if m := open(); m["ok"] != false || !strings.Contains(toText(m), "inside the encrypted archive") {
		t.Fatalf("a folder inside an encrypted archive: %v", m)
	}
	picked = empty
	if m := open(); m["ok"] != false || !strings.Contains(toText(m), "isn't a MailArchive archive") {
		t.Fatalf("not an archive: %v", m)
	}
	picked = ""
	if m := open(); m["canceled"] != true {
		t.Fatalf("cancelled: %v", m)
	}
	page, _ := getPage(t, DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "none.json")}))
	assure.Reached(t, strings.Contains(page, `data-open-archive`) && strings.Contains(page, OpenArchiveLabel), "the first card offers it")
}

// covers: MA-805, MA-739, R37
// The Danger zone's uninstall locks the Desktop's own open archives before it plans,
// so their readers' working copies are never met as another program's; the preview
// leaves unlocking possible, the real run does not.
func TestUninstallLocksTheDesktopsOwnArchives(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	var openAtPlan bool
	uninstallRun = func(string, []string, string, bool) (*uninstall.Report, error) {
		openAtPlan = e.cfg.Keys.get(out) != nil
		return &uninstall.Report{}, nil
	}
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	e.read("/api/search?q=hello") // its reader is made
	d := newDashboard(Config{ConfigPath: filepath.Join(t.TempDir(), "graph-config.json"), Keys: e.cfg.Keys})
	postUninstall(t, d, `{"confirm":false}`)
	if openAtPlan || e.cfg.Keys.get(out) != nil {
		t.Fatal("the preview planned with the Desktop's own archive open")
	}
	if _, err := e.cfg.Keys.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatalf("after a preview, unlocking still works: %v", err)
	}
	postUninstall(t, d, `{"confirm":true}`)
	_, err := e.cfg.Keys.unlock(out, keysPassword, keyring.OpenOptions{})
	assure.Reached(t, !openAtPlan && errors.Is(err, errKeysClosed), "the real run locks and refuses every unlock after")
}

// covers: MA-805, R37
// One derivation at a time per archive, and a lock wins over an unlock in flight
// (§3.2, §3.7): a second unlock while one derives is refused; a Lock that lands while
// it derives destroys its key, nothing kept open.
func TestAnUnlockInFlightLosesToALock(t *testing.T) {
	out := backedUpEncrypted(t)
	k := newArchiveKeys(nil, readerLockedNote)
	entered, release := make(chan struct{}), make(chan struct{})
	orig := openArchive
	openArchive = func(abs, typed string, o keyring.OpenOptions) (*keyring.Unlocked, error) {
		close(entered)
		<-release
		return orig(abs, typed, o)
	}
	defer func() { openArchive = orig }()
	errc := make(chan error, 1)
	go func() { _, err := k.unlock(out, keysPassword, keyring.OpenOptions{}); errc <- err }()
	<-entered
	if _, err := k.unlock(out, keysPassword, keyring.OpenOptions{}); !errors.Is(err, errUnlockBusy) {
		t.Fatalf("a second unlock while one derives: %v", err)
	}
	k.lock(out)
	close(release)
	err := <-errc
	assure.Reached(t, errors.Is(err, keyring.ErrLocked) && k.get(out) == nil, "the lock won; nothing kept open")
}
