package desktop

import (
	"context"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/server"
)

// The P3-8 friction review's Desktop findings (docs/review-encrypted-archives-friction.md):
// B25, B26, B7's follow-through, T2-12, the plaintext golden and the Tier C lows.

// newKeysEnvM365 is newKeysEnv with Microsoft 365 set up on this computer.
func newKeysEnvM365(t *testing.T, out string) *keysEnv {
	t.Helper()
	dir := t.TempDir()
	settings := filepath.Join(dir, "desktop-settings.json")
	if err := SaveSettings(settings, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "contoso", ClientID: "APPID", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	store := graphconfig.NewFileSecretStore(t.TempDir())
	cfg := Config{SettingsPath: settings, ConfigPath: cfgPath, IndexPath: filepath.Join(dir, "known-archives.json"), Store: store, Keys: newArchiveKeys(store, readerLockedNote)}
	sess := localweb.NewSession()
	h := dashboardHandler(cfg, loopback(8097), sess, nil)
	m := dashCSRFRe.FindStringSubmatch(sessionPage(t, h))
	if m == nil {
		t.Fatal("no CSRF token on the page")
	}
	rm := ReaderHandler(cfg)
	t.Cleanup(func() { cfg.Keys.lockAll(true); rm.(*readerMux).Close() })
	return &keysEnv{h: h, reader: rm, csrf: m[1], cfg: cfg, out: out}
}

// rawPost is a POST through the dashboard's session with the page's token when csrf is
// not "", returning the raw body.
func rawPost(t *testing.T, h http.Handler, path, csrf string) (int, string) {
	t.Helper()
	s, _ := localweb.Guarded(h)
	req := httptest.NewRequest(http.MethodPost, s.Session.Prefix()+path, strings.NewReader("{}"))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	req.AddCookie(sessionCookie(t, h, s.Session, req.Host))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// healthText is the Status & health reply as text.
func healthText(t *testing.T, h http.Handler) string {
	t.Helper()
	_, m := sessionRequest(t, h, http.MethodGet, "api/health", "")
	return toText(m)
}

// covers: MA-852, R37
// B25: an encrypted archive made from local mail is not shown as a Microsoft 365 one.
// The positive twin first: a plaintext Microsoft 365 archive keeps its heading and
// Archive now. An encrypted archive with no Microsoft 365 set up here is local-only —
// its heading, the Microsoft 365 card, Archive now and the new-mail line on Status &
// health all say so, in words for an encrypted archive — and a backup job set up here
// for one makes it local even with Microsoft 365 set up, since such a job reads local
// mail only. Archive now is refused for any encrypted archive, in those words, and
// nothing starts.
func TestAnEncryptedLocalArchiveIsNotShownAsMicrosoft365(t *testing.T) {
	h, _, settings := dashboardFixture(t)
	m365 := archiveAt(t, t.TempDir(), "M365", map[string]string{"mailbox:u@contoso.com": "b"})
	SaveSettings(settings, Settings{Out: m365})
	if page := sessionPage(t, h); !strings.Contains(page, "Your Microsoft 365 mail archive") || !strings.Contains(page, `id="archiveBtn"`) {
		t.Fatal("positive twin: a Microsoft 365 archive keeps its heading and Archive now")
	}

	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	if !e.cfg.localOnly() {
		t.Fatal("an encrypted archive with no Microsoft 365 set up here is local-only")
	}
	page := sessionPage(t, e.h)
	for _, bad := range []string{"Your Microsoft 365 mail archive", `id="archiveBtn"`, `id="m365Form"`} {
		if strings.Contains(page, bad) {
			t.Fatalf("an encrypted local archive's page shows %s", bad)
		}
	}
	if !strings.Contains(page, "Your encrypted mail archive") || !strings.Contains(page, `id="archiveNowOff"`) || !strings.Contains(page, "Not used by this archive") {
		t.Fatal("an encrypted local archive's page does not say what it is")
	}
	if h := healthText(t, e.h); !strings.Contains(h, "does not fetch Microsoft 365 mail into an encrypted archive") {
		t.Fatalf("the health reply's new-mail line: %s", h)
	}
	ptr := filepath.Join(out, container.PointerName)
	before, _ := os.ReadFile(ptr)
	code, m := e.post(t, "api/capture", nil)
	after, _ := os.ReadFile(ptr)
	_, act := sessionRequest(t, e.h, http.MethodGet, "api/activity", "")
	assure.Refused(t, code, toText(m), assure.Code(http.StatusOK), assure.Names("Archive now is off", "encrypted", "in a terminal"),
		assure.NoSideEffect(func() bool { return string(before) == string(after) && act["running"] != true }))

	// With Microsoft 365 set up here, a backup job set up for it still makes it local.
	box := filepath.Join(t.TempDir(), "Later")
	os.WriteFile(box, []byte(laterMbox), 0o600)
	e2 := newKeysEnvM365(t, out)
	if e2.cfg.localOnly() {
		t.Fatal("positive twin: with Microsoft 365 set up and no backup job, an encrypted archive is not known to be local")
	}
	if page := sessionPage(t, e2.h); strings.Contains(page, `id="archiveBtn"`) || !strings.Contains(page, "Your encrypted mail archive") {
		t.Fatal("Archive now is offered for an encrypted archive")
	}
	runJobFor(t, out, box)
	assure.Reached(t, e2.cfg.localOnly() && strings.Contains(sessionPage(t, e2.h), "Not used by this archive"), "a backup job set up here makes it local")
}

// covers: MA-852, R37
// B26: an encrypted archive made at the command line has no backup job here, so its
// Weekly backup card offers no Run now and names the exact command that backs it up,
// and a Run now sent anyway is refused in plain words with the same command, nothing
// written. The positive twin: with a backup job set up here and the archive unlocked,
// Run now is offered, runs, and its result says to read the messages here, with Open
// archive — not "open it in MailArchive Desktop" to someone already in it.
func TestAnEncryptedArchiveMadeAtTheCommandLineNamesItsBackupCommand(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	cmd := shellJoin(AddMailArgv("<the mail folder or file it was made from>", out), e.cfg.goos())
	page := sessionPage(t, e.h)
	card := page[strings.Index(page, `id="backupEncrypted"`):]
	card = card[:strings.Index(card, "</p>")]
	if strings.Contains(page, `id="runBackupBtn"`) || !strings.Contains(card, template.HTMLEscapeString(cmd)) {
		t.Fatalf("a command-line archive's Weekly backup card names its command %q and offers no Run now:\n%s", cmd, card)
	}
	ptr := filepath.Join(out, container.PointerName)
	before, _ := os.ReadFile(ptr)
	code, m := e.post(t, "api/run-backup", nil)
	after, _ := os.ReadFile(ptr)
	assure.Refused(t, code, toText(m), assure.Code(http.StatusOK), assure.Names("Run now has nothing to run", "mailarchive -input", "-out", "password"),
		assure.Forbid("run-job"), assure.NoSideEffect(func() bool { return string(before) == string(after) }))
	var reply map[string]any
	json.Unmarshal([]byte(toText(m)), &reply)
	if !strings.Contains(reply["error"].(string), cmd) {
		t.Fatalf("the refusal names the exact command %q: %v", cmd, reply["error"])
	}

	box := filepath.Join(t.TempDir(), "Later")
	os.WriteFile(box, []byte(laterMbox), 0o600)
	runJobFor(t, out, box)
	if page := sessionPage(t, e.h); !strings.Contains(page, `id="runBackupBtn"`) || strings.Contains(page, template.HTMLEscapeString(cmd)) {
		t.Fatal("positive twin: with a backup job set up here, Run now is offered")
	}
	if code, m := e.post(t, "api/run-backup", nil); code != http.StatusOK || m["ok"] != true {
		t.Fatalf("run now: %d %v", code, m)
	}
	result, errText := waitIdle(t, e)
	assure.Reached(t, errText == "" && strings.Contains(result, "Backup complete") && strings.Contains(result, "Open archive") &&
		!strings.Contains(result, "open it in MailArchive Desktop"), "Run now's result, said to someone in MailArchive Desktop")
}

// covers: MA-852, R37
// B7's follow-through: Run now shows the running job's Cancel (and Skip) beside its own
// note. The page script is not run by go test, so its lines are pinned: pollBackup asks
// jobButtons to place and show them, on every poll before it returns while the job
// runs, and jobButtons reveals Cancel exactly when the job is running and cancellable.
// The buttons and Run now's note are on an encrypted archive's page with a backup job.
func TestRunNowShowsItsCancelBesideItsNote(t *testing.T) {
	js := dashboardJS
	i := strings.Index(js, "async function pollBackup(){")
	if i < 0 {
		t.Fatal("no pollBackup in the page script")
	}
	poll := js[i:]
	poll = poll[:strings.Index(poll, "\n}\n")]
	place, running := strings.Index(poll, "jobButtons(d,n)"), strings.Index(poll, "if(d.running){setTimeout(pollBackup")
	if place < 0 || running < 0 || place > running {
		t.Fatalf("pollBackup does not show the job's buttons beside Run now's note before it polls again:\n%s", poll)
	}
	j := strings.Index(js, "function jobButtons(d,at){")
	if j < 0 {
		t.Fatal("no jobButtons in the page script")
	}
	jb := js[j:]
	jb = jb[:strings.Index(jb, "\n}\n")]
	for _, want := range []string{"at.after(sk,cn)", "cn.hidden=!(d.running&&d.cancellable)", "sk.hidden=!(d.running&&d.skippable)"} {
		if !strings.Contains(jb, want) {
			t.Fatalf("jobButtons does not %s:\n%s", want, jb)
		}
	}
	out := backedUpEncrypted(t)
	box := filepath.Join(t.TempDir(), "Later")
	os.WriteFile(box, []byte(laterMbox), 0o600)
	runJobFor(t, out, box)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	page := sessionPage(t, e.h)
	assure.Reached(t, strings.Contains(page, `id="runBackupBtn"`) && strings.Contains(page, `id="runBackupNote"`) && strings.Contains(page, `id="jobCancel"`), "Run now, its note and Cancel on the page")
}

// covers: MA-852, R37
// T2-12: switching away from an encrypted archive open here locks it, and the page says
// so before the switch — on Your archives and beside the archive location in Settings —
// naming the archive. Locked, there is nothing to say (the positive twin); after the
// switch it is locked, as said.
func TestSwitchingSaysItLocksTheArchiveFirst(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	other := archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"})
	if err := archindex.WriteFile(e.cfg.IndexPath, archindex.Index{Entries: []archindex.Entry{
		{Path: out, AddedAt: at0, LastSeenAt: at0}, {Path: other, AddedAt: at0, LastSeenAt: at0}}}); err != nil {
		t.Fatal(err)
	}
	if page := sessionPage(t, e.h); strings.Contains(page, `id="switchLocks"`) || !strings.Contains(page, "data-switch-archive") {
		t.Fatal("positive twin: a locked archive's switch has nothing to say")
	}
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	page := sessionPage(t, e.h)
	said := "Switching to another archive locks " + template.HTMLEscapeString(out) + "."
	for _, id := range []string{`id="switchLocks"`, `id="switchLocksSettings"`} {
		k := strings.Index(page, id)
		if k < 0 || !strings.Contains(page[k:], said) {
			t.Fatalf("%s does not say %q", id, said)
		}
	}
	e.post(t, "api/settings", map[string]any{"out": other})
	assure.Reached(t, e.cfg.Keys.get(out) == nil, "the switch locked it, as said")
}

// goldenMbox is a message whose subject, sender and attachment name are markers no
// page or reply of the Desktop may show without the reader.
const goldenMbox = "From - Mon Jan 01 00:00:00 2024\r\n" +
	"From: Ulyana Plumtree <ulyana.plumtree@example.org>\r\nSubject: Zebra-Quartz ledger 7731\r\n" +
	"Date: Mon, 01 Jan 2024 10:00:00 +0000\r\nMessage-ID: <golden7731@example.org>\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n" +
	"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nthe numbers are inside\r\n" +
	"--b1\r\nContent-Type: application/pdf; name=\"secret-plan-8842.pdf\"\r\n" +
	"Content-Disposition: attachment; filename=\"secret-plan-8842.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQK\r\n" +
	"--b1--\r\n"

// covers: MA-852, R37
// The deferred golden (P3-8, deferred decision 3): the Overview page and the Status &
// health reply carry no mail-derived text — locked or unlocked — though the archive
// holds a message whose subject, sender and attachment name are planted markers. The
// positive twin: the reader, unlocked, finds every marker, so they are in the archive.
// The reader's pages say K9's line where they offer a download (B8): the search page,
// the message page beside its zip and .eml, and the folder page listing the zip.
func TestTheOverviewAndHealthCarryNoMailText(t *testing.T) {
	out := encryptedWith(t, goldenMbox)
	e := newKeysEnv(t, out)
	markers := []string{"Zebra", "Quartz", "7731", "Ulyana", "Plumtree", "ulyana.plumtree", "secret-plan", "8842"}
	check := func(state string) {
		t.Helper()
		page, health := sessionPage(t, e.h), healthText(t, e.h)
		for _, mk := range markers {
			if strings.Contains(page, mk) || strings.Contains(health, mk) {
				t.Fatalf("%s: the Overview or Status & health carries %q", state, mk)
			}
		}
	}
	check("locked")
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	check("unlocked")

	code, body := e.read("/api/search?q=Zebra")
	for _, mk := range []string{"Zebra-Quartz ledger 7731", "Ulyana Plumtree"} {
		if code != http.StatusOK || !strings.Contains(body, mk) {
			t.Fatalf("positive twin: the reader finds %q: %d %s", mk, code, body)
		}
	}
	var res struct{ Results []struct{ Path string } }
	json.Unmarshal([]byte(body), &res)
	if len(res.Results) != 1 {
		t.Fatalf("one result: %s", body)
	}
	if _, root := e.read("/"); !strings.Contains(root, server.DownloadNotEncrypted) {
		t.Fatal("the search page does not say a download is not encrypted")
	}
	p := res.Results[0].Path
	code, msg := e.read("/files/" + p)
	if code != http.StatusOK || !strings.Contains(msg, "secret-plan-8842.pdf") || !strings.Contains(msg, "download zip</a> <span class=\"mailarchive-k9\">("+server.DownloadNotEncrypted) ||
		!strings.Contains(msg, "Original message (.eml)</a> <span class=\"mailarchive-k9\">("+server.DownloadNotEncrypted) {
		t.Fatalf("the message page's downloads: %d\n%s", code, msg)
	}
	dir := p[:strings.LastIndex(p, "/")+1]
	code, folder := e.read("/files/" + dir)
	assure.Reached(t, code == http.StatusOK && strings.Contains(folder, "Zebra-Quartz") && strings.Contains(folder, `<p class="muted mailarchive-k9">`+server.DownloadNotEncrypted), "the folder page lists the zip with K9's line")
}

// encryptedWith makes an encrypted archive holding the messages of mbox, with its index
// — backedUpEncrypted with mail of the test's own.
func encryptedWith(t *testing.T, mbox string) string {
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
	os.WriteFile(box, []byte(mbox), 0o600)
	opts := app.Options{Inputs: []string{box}, Out: out, Index: true, KeepRaw: true}
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

// covers: MA-852, R37
// Tier C lows on an encrypted archive's page: the sidebar links to the Encryption card
// the locked reader sends people to; the Backup alerts card, for scheduled backups an
// encrypted archive cannot have, is not shown (the positive twin: a plaintext archive's
// is); the create form's button says what it does; the copies checkbox says what this
// build keeps — a development build no promise of three platforms and 134 MB; a
// key-change question travels in its own field, not as an error; and a request that did
// not come from the page is refused in plain words, nothing run.
func TestAnEncryptedArchivesPageSpeaksPlainly(t *testing.T) {
	plain := getOverview(t, Config{ConfigPath: filepath.Join(t.TempDir(), "none.json")})
	if !strings.Contains(plain, `id="sec-alerts"`) || !strings.Contains(plain, `href="#sec-alerts"`) || strings.Contains(plain, `href="#sec-encryption"`) {
		t.Fatal("positive twin: a plaintext page keeps Backup alerts and has no Encryption link")
	}
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	orig := releaseBuild
	t.Cleanup(func() { releaseBuild = orig })
	releaseBuild = func() bool { return false }
	page := sessionPage(t, e.h)
	if !strings.Contains(page, `href="#sec-encryption"`) || !strings.Contains(page, `id="sec-encryption"`) {
		t.Fatal("the sidebar does not link to the Encryption card")
	}
	if strings.Contains(page, `id="sec-alerts"`) || strings.Contains(page, `href="#sec-alerts"`) {
		t.Fatal("Backup alerts is shown for an archive that cannot be scheduled")
	}
	if strings.Contains(page, "Make its key") || !strings.Contains(page, "Continue — show the recovery key") {
		t.Fatal("the create form's button")
	}
	if strings.Contains(page, "134 MB") || strings.Contains(page, "Keep a copy of MailArchive for Mac, Windows and Linux") || !strings.Contains(page, "This is a development build of MailArchive: it keeps only a copy of itself") {
		t.Fatal("a development build's copies checkbox promises a release's copies")
	}
	releaseBuild = func() bool { return true }
	if page := sessionPage(t, e.h); !strings.Contains(page, "Keep a copy of MailArchive for Mac, Windows and Linux") || strings.Contains(page, "development build") {
		t.Fatal("positive twin: a release's checkbox names its copies")
	}

	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, m := e.post(t, "api/encryption/change-password", map[string]any{"newPassword": "another long passphrase", "again": "another long passphrase"})
	if m["confirm"] != "keychange" || m["question"] == nil || m["question"] == "" || m["error"] != nil {
		t.Fatalf("a key-change question in its own field: %v", m)
	}
	if !strings.Contains(encryptionJS, "confirm(d.question||d.error)") {
		t.Fatal("the page script does not ask the question from its own field")
	}

	ptr := filepath.Join(out, container.PointerName)
	before, _ := os.ReadFile(ptr)
	code, body := rawPost(t, e.h, "api/run-backup", "")
	after, _ := os.ReadFile(ptr)
	assure.Refused(t, code, body, assure.Code(http.StatusForbidden), assure.Names("refused", "MailArchive Desktop page", "Reload the page"),
		assure.Forbid("tokened", "same-origin"), assure.NoSideEffect(func() bool { return string(before) == string(after) }))
}

// covers: MA-852, R37
// A finished unencrypted copy counts in plain English: "1 of 1 message", "1 was left
// out", "2 were left out" (P3-8, Tier C).
func TestACopysCountsReadAsEnglish(t *testing.T) {
	one := copiedLine("/c", 1, 1, 0)
	if !strings.Contains(one, "1 of 1 message copied.") || strings.Contains(one, "left out") {
		t.Fatalf("one message: %q", one)
	}
	if got := copiedLine("/c", 1, 2, 1); !strings.Contains(got, "1 of 2 messages copied.") || !strings.Contains(got, " 1 was left out") {
		t.Fatalf("one left out: %q", got)
	}
	got := copiedLine("/c", 1, 3, 2)
	assure.Reached(t, strings.Contains(got, " 2 were left out"), "two left out")
}
