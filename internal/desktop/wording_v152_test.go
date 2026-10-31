package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
)

// covers: MA-938, S41
// The Desktop's words match what it does in 1.5.2 (owner request 2026-10-31: "make
// sure all the documents help files popups and page wording is updated to match the
// newest functionality"). Each sentence is pinned whole, so a later edit that brings
// back the old claim turns this red.
//
// The positive twin first: with Microsoft 365 set up and a Microsoft 365 archive
// chosen, the Microsoft 365 tile and Archive now keep their Microsoft 365 words.
// Then, with no archive and no connection: the Microsoft 365 tile is neutral, not a
// warning; Archive now says what it is for and the real steps; the Security & privacy
// card no longer claims MailArchive can never send or delete mail (the Thunderbird
// sync can) or that mail goes only between Microsoft and "this PC"; the archive
// location's example is this computer's own; the Export, Maintenance, Recent backups,
// Backup alerts and Uninstall texts name what they really do. A local-files archive's
// Microsoft 365 card says it is not used, without a warning. Maintenance with no
// archive, Archive now with no connection and the reader with no search index each say
// what to do in words that exist on the page.
func TestDesktopWordsMatchWhatItDoes(t *testing.T) {
	// Positive twin: a Microsoft 365 archive with its connection keeps the Microsoft 365
	// words on the tile and the Archive now card.
	dir := t.TempDir()
	m365 := archiveAt(t, dir, "M365", map[string]string{"mailbox:u@contoso.com": "b"})
	h, _, settings := dashboardFixture(t)
	SaveSettings(settings, Settings{Out: m365})
	page := sessionPage(t, h)
	for _, want := range []string{
		`<div class="label">Microsoft 365</div><div class="value warn">Sign-in needed</div>`,
		"Fetch the latest mail into your archive. The first time, you sign in with a Microsoft device code shown here.",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("positive twin: a Microsoft 365 archive's page lacks %q", want)
		}
	}

	// No archive, no connection.
	body := getOverview(t, Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	for _, want := range []string{
		`<div class="label">Microsoft 365</div><div class="value ">Not set up (only needed for Microsoft 365 mail)</div>`,
		"Archive now copies new mail from a Microsoft 365 mailbox into your archive. To use it, fill in the Microsoft 365 connection card, type a folder for the archive in Settings → Archive location and click Save settings, then come back here. For Thunderbird, Outlook or other mail files, you don't need this button.",
		"MailArchive Desktop has no archive yet. To use one you already have, click <b>Open an archive folder…</b> above.",
		"<b>Runs on this computer.</b> The control panel and your archive are local; nothing here is served to the network.",
		"<b>Read-only to your mail, with one exception you choose.</b> MailArchive only reads your mail files and, for Microsoft 365, makes only read requests (Mail.Read): it never sends, moves, changes or deletes your mail. The exception is the Thunderbird sync, if you turn it on. It opens and closes your real Thunderbird, which then does what it always does: it checks your accounts, sends anything in your Outbox, runs your filters, and on closing may empty Trash. The Settings card explains it.",
		"<b>No MailArchive servers.</b> MailArchive reads your mail from Microsoft 365 or from mail files on this computer, and sends nothing to us. It connects elsewhere only for two things: backup alert emails, if you turn them on, go through the mail server you choose; and an encrypted archive that keeps copies of MailArchive (ticked by default when you make one) downloads them from MailArchive's GitHub releases page.",
		"Your data stays on this computer; any secret is kept in",
		`placeholder="The archive's folder, e.g. ` + app.ArchiveExampleFor(Config{}.goos()) + `"`,
		"Copy selected folders and messages from your Microsoft 365 mailbox, or from a MailArchive archive, to a folder, with a manifest and checksums the recipient can check.",
		"Occasional housekeeping. These share the archive with backups, so only one runs at a time. Progress shows in the activity log on the Archive now card.",
		"No backups have run yet. When a backup finishes (scheduled, Run now or Archive now), its result appears here.",
		"Email someone about your scheduled backups. Tick what you want below: a failed backup (one email per run of failures, then one more when backups work again), Thunderbird not synced for 3 days, an auto-paused backup, or every success. MailArchive sends the email itself through the mail server you fill in below; there is nothing else to install.",
		`title="Remove MailArchive's scheduled backups, settings and sign-in from this computer. Your saved mail archives are never deleted. You remove the program itself afterwards; the last step tells you how."`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Overview (no archive, no connection) lacks %q", want)
		}
	}
	for _, bad := range []string{"this PC", "It can never send, move, change, or delete", "No third-party servers", `value warn">Not configured`, `C:\MailArchive, a mapped drive`} {
		if strings.Contains(body, bad) {
			t.Errorf("the Overview (no archive, no connection) still says %q", bad)
		}
	}

	// A local-files archive: the Microsoft 365 card says it is not used, with no warning,
	// and the scheduled backup it can have re-reads the files it was made from.
	out := tbArchive(t)
	lh := DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: out, GOOS: "darwin"})
	lp, _ := getPage(t, lh)
	for _, want := range []string{
		// Amended 2026-10-31 (MA-945's friction review M2): the card gives the guide's steps,
		// not a terminal command (TestTheMicrosoft365CardAndTheGuideAgree drives them).
		`<div class="v">` + m365LocalOnlyLine + `</div>`,
		"No scheduled backup is set up for this archive yet. A scheduled backup keeps it up to date automatically: it re-reads the mail files this archive was made from and adds anything new.",
	} {
		if !strings.Contains(lp, want) {
			t.Errorf("a local-files archive's page lacks %q", want)
		}
	}

	// The Extraction workspace's sidebar has the Overview's groups: Recent backups under
	// Diagnostics, Microsoft 365 connection and Backup alerts under Setup.
	ws := apiGet(DashboardHandler(Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")}), "/workspace")
	setup := strings.Index(ws, `<div class="nav-title">Setup</div>`)
	for _, want := range []string{`<a href="./#sec-history">Recent backups</a>`, `<a href="./#sec-m365">Microsoft 365 connection</a>`, `<a href="./#sec-alerts">Backup alerts</a>`} {
		if i := strings.Index(ws, want); i < 0 {
			t.Errorf("the workspace sidebar lacks %q", want)
		} else if want != `<a href="./#sec-history">Recent backups</a>` && i < setup {
			t.Errorf("the workspace sidebar has %q before Setup", want)
		}
	}

	// The encrypted note names the Encryption card's real restore button: the button draws
	// its words from the note's constant (MA-940), so they cannot drift.
	src, err := os.ReadFile("dashboard.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `id="encRestoreFileBtn" title="Choose the recovery file saved with this archive's recovery key.">`+"` + app.RestoreKeyWords + `"+`</button>`) ||
		!strings.Contains(app.EncryptedNote(), `"`+app.RestoreKeyWords+`"`) {
		t.Error("the restore button and the encrypted note do not share app.RestoreKeyWords")
	}

	// Maintenance with no archive: says what to do with the page's own button.
	nh := DashboardHandler(Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	_, ncsrf := getPage(t, nh)
	const noArchive = "No archive is chosen yet, so there is nothing to work on. Click Open an archive folder… at the top of the page, or type the archive's folder in Settings → Archive location and click Save settings, then try again."
	for _, p := range []string{"/api/verify", "/api/rebuild"} {
		r := postAction(nh, p, ncsrf, "127.0.0.1:8097", "http://127.0.0.1:8097")
		if !strings.Contains(r.Body.String(), noArchive) {
			t.Errorf("%s with no archive = %s", p, r.Body.String())
		}
	}

	// Archive now with an archive folder chosen but no Microsoft 365 connection: names
	// the card and button that exist on the page, and starts nothing.
	ch := DashboardHandler(Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: t.TempDir()})
	_, ccsrf := getPage(t, ch)
	r := postAction(ch, "/api/capture", ccsrf, "127.0.0.1:8097", "http://127.0.0.1:8097")
	if !strings.Contains(r.Body.String(), "Microsoft 365 isn't set up yet. Fill in the Microsoft 365 connection card on this page and click Save configuration, then click Archive now again.") {
		t.Errorf("Archive now with no connection = %s", r.Body.String())
	}
	if a := apiGet(ch, "/api/activity"); strings.Contains(a, `"running":true`) {
		t.Errorf("a refused Archive now started something: %s", a)
	}
}
