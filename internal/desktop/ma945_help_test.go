package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/presync"
)

// helpCardView is the "How to use MailArchive" card of an Overview page: its visible
// text (whitespace collapsed, entities decoded), every href inside it, and which of
// its three parts render open.
type helpCardView struct {
	text    string
	hrefs   []string
	parts   []string        // the summaries, in order
	open    map[string]bool // summary -> open
	count   int             // how many elements carry id="sec-help"
	outside string          // the page's visible text outside the card
}

func readHelpCard(t *testing.T, page string) helpCardView {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("the page does not parse: %v", err)
	}
	attr := func(n *xhtml.Node, k string) (string, bool) {
		for _, a := range n.Attr {
			if a.Key == k {
				return a.Val, true
			}
		}
		return "", false
	}
	var textOf func(n *xhtml.Node, b *strings.Builder, skip *xhtml.Node)
	textOf = func(n *xhtml.Node, b *strings.Builder, skip *xhtml.Node) {
		if n == skip {
			return
		}
		if n.Type == xhtml.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		// A block element is a break in the text; an inline one (b, a, span) is not.
		block := n.Type == xhtml.ElementNode && !map[string]bool{"b": true, "a": true, "span": true, "i": true, "em": true, "strong": true}[n.Data]
		if block {
			b.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			textOf(c, b, skip)
		}
		if block {
			b.WriteString(" ")
		}
	}
	collapse := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	var h helpCardView
	h.open = map[string]bool{}
	var card *xhtml.Node
	var find func(n *xhtml.Node)
	find = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			if id, _ := attr(n, "id"); id == "sec-help" {
				h.count++
				if card == nil {
					card = n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	if card == nil {
		return h
	}
	var b strings.Builder
	textOf(card, &b, nil)
	h.text = collapse(b.String())
	var o strings.Builder
	textOf(root, &o, card)
	h.outside = collapse(o.String())
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			if href, ok := attr(n, "href"); ok {
				h.hrefs = append(h.hrefs, href)
			}
			if n.Data == "details" {
				_, isOpen := attr(n, "open")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == xhtml.ElementNode && c.Data == "summary" {
						var s strings.Builder
						textOf(c, &s, nil)
						name := collapse(s.String())
						h.parts = append(h.parts, name)
						h.open[name] = isOpen
						break
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(card)
	return h
}

// helpPageStates are the Overview's states the sidebar entry must be on in: no archive
// (with and without a Microsoft 365 connection), an archive of local mail files, a
// Microsoft 365 archive, an encrypted archive, and the Extraction workspace turned off.
func helpPageStates(t *testing.T) map[string]Config {
	t.Helper()
	dir := t.TempDir()
	tenant := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(tenant, &graphconfig.Config{Tenant: "contoso", ClientID: "APPID", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	none := filepath.Join(dir, "none.json")
	base := func(config, out string) Config {
		return Config{ConfigPath: config, SettingsPath: filepath.Join(t.TempDir(), "s.json"),
			TokenCachePath: filepath.Join(t.TempDir(), "tok.json"), Out: out, BackupInstalled: func() bool { return false }}
	}
	off := base(none, "")
	off.WorkspaceOff = true
	return map[string]Config{
		"no archive":                       base(none, ""),
		"no archive, Microsoft 365 set up": base(tenant, ""),
		"a local-files archive":            base(none, archiveAt(t, dir, "Local", map[string]string{"path:/mail/Local Folders": "b"})),
		"a Microsoft 365 archive":          base(tenant, archiveAt(t, dir, "M365", map[string]string{"mailbox:u@contoso.com": "b"})),
		"an encrypted archive":             base(none, vaultedArchive(t)),
		"the Extraction workspace off":     off,
	}
}

// covers: MA-945, R12, S41
// "How to use MailArchive" is the sidebar's first entry from the moment MailArchive
// Desktop starts, whatever the archive state, on every page: on the Overview it jumps
// to the help card on the same page (MA-302's same-page rule), on the Extraction
// workspace and Export activity pages to the Overview's card. The card is on the page
// exactly once, with its three parts in order, and every link inside it names a card
// that is on that page. With no archive chosen, the "No archive chosen yet" card points
// to it and "Getting started" is open; with an archive, nothing is open.
func TestHowToUseMailArchiveIsOnEveryPage(t *testing.T) {
	const pointer = "New to MailArchive? Read How to use MailArchive below: it explains each part of this page, what Thunderbird needs, and how to connect Microsoft 365."
	// The friction review's O5 (applied by the lead, 2026-10-31): "The parts of this page" is
	// a part of its own, closed, so Getting started opens short.
	wantParts := []string{"Getting started", "The parts of this page", "Thunderbird", "Microsoft 365"}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		for name, cfg := range helpPageStates(t) {
			cfg.GOOS = goos
			what := goos + ", " + name
			page := getOverview(t, cfg)
			nav, ids, _ := sidebarNav(t, page)
			assure.Reached(t, nav, what+": the sidebar")
			if first := nav[0]; first.text != "How to use MailArchive" || first.href != "#sec-help" || first.id != "navHelp" {
				t.Errorf("%s: the sidebar's first entry is %+v, want How to use MailArchive → #sec-help", what, first)
			}
			h := readHelpCard(t, page)
			if h.count != 1 {
				t.Fatalf("%s: %d elements carry id sec-help, want 1", what, h.count)
			}
			if strings.Join(h.parts, "|") != strings.Join(wantParts, "|") {
				t.Errorf("%s: the card's parts are %q, want %q", what, h.parts, wantParts)
			}
			assure.Reached(t, h.hrefs, what+": the card's links")
			for _, href := range h.hrefs {
				if !strings.HasPrefix(href, "#") || !ids[href[1:]] {
					t.Errorf("%s: the card links to %q, which is not a card on this page", what, href)
				}
			}
			// Backup alerts is a link wherever its card is drawn (every archive but an
			// encrypted one), like every other part the guide lists (friction review L7).
			linksAlerts := false
			for _, href := range h.hrefs {
				linksAlerts = linksAlerts || href == "#sec-alerts"
			}
			if want := name != "an encrypted archive"; linksAlerts != want {
				t.Errorf("%s: the guide links to Backup alerts = %v, want %v", what, linksAlerts, want)
			}
			noArchive := cfg.Out == ""
			if got := strings.Contains(h.outside, pointer); got != noArchive {
				t.Errorf("%s: the Overview's pointer to the guide shown = %v, want %v", what, got, noArchive)
			}
			if noArchive && !strings.Contains(page, `<a href="#sec-help">How to use MailArchive</a> below`) {
				t.Errorf("%s: the no-archive pointer is not a link to the guide", what)
			}
			if h.open["Getting started"] != noArchive || h.open["The parts of this page"] || h.open["Thunderbird"] || h.open["Microsoft 365"] {
				t.Errorf("%s: open parts %v, want Getting started open only with no archive", what, h.open)
			}
			// Getting started keeps "Your first archive"; the page's parts are no longer in it.
			if i := strings.Index(page, `<details id="helpStart"`); i < 0 {
				t.Errorf("%s: no Getting started part", what)
			} else if gs := page[i : i+strings.Index(page[i:], "</details>")]; !strings.Contains(gs, "<b>Your first archive.</b>") || strings.Contains(gs, `href="#sec-settings">Settings</a>: <b>Archive location</b>`) {
				t.Errorf("%s: Getting started does not keep Your first archive alone:\n%s", what, gs)
			}
		}
	}

	// The Extraction workspace and Export activity pages: the same first entry, to the
	// Overview's card.
	e := newWsEnv(t)
	for _, p := range []string{"/workspace", "/exports"} {
		rec := e.get(p)
		if rec.Code != 200 {
			t.Fatalf("GET %s: %d", p, rec.Code)
		}
		nav, _, _ := sidebarNav(t, rec.Body.String())
		assure.Reached(t, nav, p+": the sidebar")
		if first := nav[0]; first.text != "How to use MailArchive" || first.href != "./#sec-help" {
			t.Errorf("%s: the sidebar's first entry is %+v, want How to use MailArchive → ./#sec-help", p, first)
		}
	}
	if h := readHelpCard(t, e.get("/").Body.String()); h.count != 1 {
		t.Errorf("the Overview the workspace pages point at holds %d help cards, want 1", h.count)
	}
}

// helpSentences are the guide's sentences every platform shows, pinned whole.
var helpSentences = []string{
	// Getting started
	"MailArchive keeps a copy of your mail in an archive folder on this computer. Each backup adds the mail that is new, and you can read and search the archive in your browser.",
	"Click Use this archive if the page offers it, or click Open an archive folder… and choose the archive's folder.",
	"An archive you already have (from another computer, a drive or a backup copy): click Open an archive folder… and choose its folder.",
	"Repair fixes a scheduled backup that stopped working, and Remove deletes the scheduled backup (the archive and its mail are kept). A Microsoft 365 archive uses Archive now instead of Run now.",
	"Go back in time shows your mailbox as it was on an earlier date; only a Microsoft 365 archive keeps that history, from its first backup.",
	"Status & health: whether this archive is healthy. When something needs attention, it says what is wrong and what to do.",
	// Backup alerts (truth review 2026-10-31): every alert box starts unticked, and the
	// test email is sent even with alerts off (backupalert.SendTest sets Enabled).
	"Tick Send backup alerts by email, then tick what you want to hear about (start with a failed backup, or Thunderbird not synced for 3 days). Fill in your mail server, click Save alert settings, then Send test email to check that it arrives. The test email is sent even when alerts are switched off, so check the ticks too. An encrypted archive has no Backup alerts card.",
	"Pause (make read-only) stops all changes to an archive, and Resume allows them again. Nothing is deleted either way.",
	// The recovery key is typed back (encSavedBtn, encTypeBack), and "Remember the key on
	// this computer" is ticked by default (encCreateRemember).
	"MailArchive then shows a recovery key: save it somewhere safe, such as a password manager, click I have saved the recovery key, and type it back to show it was saved. If you lose both the password and the recovery key, only a computer that remembers the archive's key can still open it; otherwise nobody can, not even you.",
	"Scheduled backups of an encrypted archive aren't available yet: back it up with Run now while it is unlocked. An encrypted archive made at the command line has no Run now: it is backed up at the command line, and its Archive now card says how.",
	"Shut down (at the top of the page): stops MailArchive Desktop. Your archives and scheduled backups are not affected.",
	// Thunderbird
	"Your mail account added to Thunderbird, and Thunderbird opened at least once, so it has downloaded your mail.",
	// Thunderbird's default, observed in the Windows lab of 2026-10-31 (Thunderbird
	// 153.6.0esr, a Gmail IMAP account added with no settings changed: no offline_download
	// line in prefs.js, and over 36,000 messages saved to disk in the first half hour).
	"For an IMAP account this is the setting Keep messages for this account on this computer. Thunderbird turns it on for a new account, so usually there is nothing to change. To check it, in Thunderbird open Account Settings (from the ≡ menu at the top right), choose your account's Synchronization & Storage page, and see that the box is ticked. POP accounts and Local Folders always keep their mail on this computer.",
	// What the mbox walk can and cannot hold (gapclass GapKindCorruptPart; an empty part is
	// a fillable gap).
	"Downloading takes time: Thunderbird saves your messages to this computer over time, and only while it is open. A message Thunderbird has not downloaded is not in the archive yet; a later backup adds it once Thunderbird has downloaded it. A message Thunderbird saved only in part (for example without a large attachment) is archived with what is there, and a later backup fills in the rest.",
	// The wizard's IMAP offer (prepareThunderbirdGUI: the quit loop, thunderbird.EnableOffline,
	// guiWaitForSync).
	"If you make the archive from an IMAP account, the MailArchive wizard may ask Prepare a full offline download first? Yes, prepare makes sure this setting is on (it asks you to quit Thunderbird if it is open, and keeps a copy of Thunderbird's settings file beside it), then waits while you open Thunderbird and it downloads your mail.",
	"MailArchive uses the Thunderbird profile that holds the mail folder this archive was made from. (Thunderbird keeps its profiles in",
	"If you use more than one Thunderbird profile, only that one is backed up and synced.",
	// Microsoft 365
	"MailArchive Desktop backs up your own Microsoft 365 mailbox. You sign in as yourself with a code from Microsoft, and MailArchive only reads your mail: it asks Microsoft for read access (the Mail.Read permission) and nothing more. Shared mailboxes and other people's mailboxes aren't available in this version.",
	"Open App registrations and click New registration. Name it mailarchive. Under supported account types, choose Accounts in this organizational directory only. Leave the redirect URI empty.",
	"On the app's Authentication page, under Advanced settings, set Allow public client flows to Yes. MailArchive signs in with a code, so it needs no client secret.",
	"On the app's API permissions page, click Add a permission, choose Microsoft Graph, then Delegated permissions, and add Mail.Read and offline_access. offline_access lets later backups run without signing in again. MailArchive needs nothing else: it never asks to change or send mail.",
	"Most organizations let each person agree to these permissions the first time they sign in. If yours does not, an admin clicks Grant admin consent on the API permissions page, once.",
	"On the app's Overview page, copy two values: the Directory (tenant) ID and the Application (client) ID. Don't copy an Object ID: Object IDs look the same and sit right beside them, but they are the wrong values.",
	"On the Microsoft 365 connection card, paste the Directory (tenant) ID into Directory (tenant) ID (your organization's domain, such as contoso.onmicrosoft.com, works too) and the Application (client) ID into Application (client) ID.",
	"Under Authentication, choose Device (per-user), then click Save configuration.",
	"Click Archive now. The first time, the card shows a code and a link to a Microsoft page. Open the link, sign in as yourself and type the code. Before you approve, check that the page names the app mailarchive. Only type a code that this page has just shown you.",
	"Step 3: keep it backed up. In Settings, choose How often (Daily or Weekly) and the time, then click Save settings. The scheduled backup uses your saved sign-in and runs even when this page is closed, as long as you stay logged in to this computer.",
	"They apply to every Microsoft 365 backup on this computer. Deleted Items and Junk Email are left out unless you tick them.",
	"App-only (admin), the card's other choice, uses a client secret instead of your own sign-in. It is for an administrator who backs up other people's mailboxes with the mailarchive command line, and it needs the Mail.Read application permission with admin consent. MailArchive Desktop's Archive now and scheduled backups always sign in as you, so choose Device (per-user) here.",
}

// helpPlatform is one platform's own sentences, and what it must never say.
type helpPlatform struct {
	goos      string
	want, not []string
}

// The sync's shared sentences, said only where the sync runs (macOS and Windows).
var helpSyncSentences = []string{
	"MailArchive only reads your mail: it never sends, moves, changes or deletes it. The exceptions are the Thunderbird sync, if you turn it on, and the wizard's offer to turn on Thunderbird's offline download (see Thunderbird below).",
	// The wizard asks about the sync after a Yes to the schedule question (offerSchedule).
	"For Thunderbird mail the wizard then asks Sync Thunderbird before each scheduled backup? (see Thunderbird below). You can also set How often and the sync later in Settings on this page.",
	"Sync Thunderbird before each backup. Thunderbird saves new mail only while it is running. With this setting on, each backup first starts Thunderbird and waits for its mail, so the archive also gets mail that had only reached the mail server. It is off until you turn it on: choose Yes — sync Thunderbird first when the MailArchive wizard asks Sync Thunderbird before each scheduled backup?, or, in Settings, choose How often, tick Sync Thunderbird before each backup, then click Save settings.",
	// A missed time is caught up on wake (Windows: StartWhenAvailable; macOS: launchd's
	// StartCalendarInterval, launchd.plist(5)); only a switched-off computer skips it.
	"A time when the computer is switched off is skipped. If it was asleep, the backup runs when it wakes up.",
	"The backup waits for its mail to stop arriving (up to 20 minutes), asks Thunderbird to quit, then backs up. A scheduled backup starts Thunderbird only when no one has used the keyboard or mouse for 2 minutes. If you are using the computer, that backup skips the sync and saves the mail already on this computer.",
	"A Thunderbird you already have open is left alone: MailArchive never starts or closes it, and the backup reads the mail it has saved.",
	"It checks all your accounts, sends anything waiting in your Outbox, runs your message filters, and may show notifications. When a scheduled backup asks it to quit, your accounts' on-exit actions run too (for example emptying Trash), so mail you deleted since the last backup may be removed before it is archived.",
	"With Hourly and the Thunderbird sync, Thunderbird stays open for about 20 minutes in an hour with no new mail, while the backup waits for mail.",
	"When the sync keeps missing: if Thunderbird could not be synced before this archive's scheduled backups for 3 days, Status & health says so and what to do, and Backup alerts sends one email, when Send backup alerts by email and a failed backup, or Thunderbird not synced for 3 days are both ticked.",
	"A softer warning, with no email, shows when Thunderbird was started before each scheduled backup for 3 days but no new mail arrived. Thunderbird may be waiting for a password or a sign-in: open Thunderbird and answer it. That warning clears by itself the next time a backup sees new mail arrive.",
}

const (
	helpMacRunNow   = "Run now syncs Thunderbird too, even while you are using the computer. It starts Thunderbird out of sight and waits for its mail (up to 20 minutes, often about 13). Click Stop waiting and back up now to back up the mail that has arrived so far. Cancel the backup skips the whole backup instead, and nothing is added to the archive. When the backup is done, Thunderbird stays open: quit it from the Dock."
	helpWinNoRunNow = "Run now: on Windows, Run now does not start Thunderbird yet. It backs up the mail already on this computer. To include the newest mail, open Thunderbird, let its mail arrive, then click Run now. Scheduled backups still sync Thunderbird first."
	helpEncSync     = "An encrypted archive is backed up only when you click Run now. If it was made from Thunderbird, its Settings card offers Sync Thunderbird before each Run now, with its own Save. Until that backup starts reading mail, Lock cancels it."
)

// helpMacWizard and helpWinWizard are how the guide, and the "No archive chosen yet" card
// (friction review M3), say to open the wizard.
const (
	helpMacWizard = "On a Mac, open Mail Archive.app and choose Archive a mail file on this computer (Outlook, Thunderbird, mbox)."
	helpWinWizard = "On Windows, open MailArchive wizard from the Start menu. If MailArchive was unzipped rather than installed, there is no Start menu entry: open the MailArchive folder, click in the address bar at the top of the window, type cmd and press Enter, then type mailarchivew.exe wizard and press Enter."
	helpLinWizard = "On Linux, run mailarchive wizard."
)

// Words no platform's guide may say any more (truth review and friction review of
// 2026-10-31): each was untrue of this version, or unchecked.
var helpGone = []string{
	"Thunderbird ticks it for a new account",
	"archived as still missing its content",
	"open MailArchive again to come back",
	"shows a recovery key once",
	"nobody can open the archive, not even you",
	"Run now backs up straight away",
	"Then come back here and click Use this archive",
	"The one exception is the Thunderbird sync",
	"Hourly: With Hourly",
	"this archive was made from (Thunderbird",
	// The lab of 2026-10-31 settled Thunderbird's default; the wizard's offer makes sure of it.
	"Yes, prepare turns this setting on for you",
	"check that the box is ticked",
}

var helpPlatforms = []helpPlatform{
	{goos: "darwin", want: append([]string{
		helpMacWizard,
		"Then come back to this page and reload it (press Command-R).",
		"Run now starts a backup now (with the Thunderbird sync on, it first waits for Thunderbird's mail; see Thunderbird below), Repair fixes",
		"Your archives and scheduled backups are not affected. To come back, open Mail Archive.app and choose Microsoft 365, folder export, browsing & tools (open MailArchive Desktop).",
		"Thunderbird installed in the Applications folder, or in the Applications folder inside your home folder. The Thunderbird sync looks for it only there.",
		"(Thunderbird keeps its profiles in ~/Library/Thunderbird/Profiles.)",
		"Scheduled backups: Thunderbird starts out of sight.",
		helpMacRunNow, helpEncSync,
		"On a Mac, the first time a scheduled backup closes Thunderbird, macOS may ask for permission to control Thunderbird; until it is answered, each run leaves Thunderbird open. To answer, open System Settings > Privacy & Security > Automation and allow the entry listed there to control Thunderbird.",
	}, helpSyncSentences...), not: []string{"does not start Thunderbird yet", "taskbar", "MailArchive does not sync Thunderbird", "off or asleep is skipped", "press F5"}},
	{goos: "windows", want: append([]string{
		helpWinWizard,
		"Then come back to this page and reload it (press F5).",
		"Run now starts a backup now, Repair fixes",
		"Your archives and scheduled backups are not affected. To come back, open MailArchive Desktop from the Start menu, or, if MailArchive was unzipped, double-click mailarchivew.exe in its folder.",
		`Thunderbird installed in its usual place: a Mozilla Thunderbird folder in Program Files (or Program Files (x86)), or in AppData\Local\Programs in your user folder. The Thunderbird sync looks for it only there.`,
		`(Thunderbird keeps its profiles in %APPDATA%\Thunderbird\Profiles.)`,
		"Scheduled backups: Thunderbird starts minimized to the taskbar.",
		helpWinNoRunNow,
	}, helpSyncSentences...), not: []string{"shows for a moment", "Stop waiting and back up now to back up","the Dock", "System Settings", "Sync Thunderbird before each Run now", "MailArchive does not sync Thunderbird", "off or asleep is skipped", "Command-R", "it first waits for Thunderbird's mail"}},
	{goos: "linux", want: []string{
		helpLinWizard,
		"Then come back to this page and reload it (press F5).",
		"Run now starts a backup now, Repair fixes",
		"MailArchive only reads your mail: it never sends, moves, changes or deletes it. The one exception is the wizard's offer to turn on Thunderbird's offline download (see Thunderbird below).",
		// Linux schedules with cron, which skips a time the computer slept through.
		"A time when the computer is off or asleep is skipped.",
		"Your archives and scheduled backups are not affected. To come back, run mailarchive desktop.",
		"(Thunderbird keeps its profiles in ~/.thunderbird.)",
		"On Linux, MailArchive does not sync Thunderbird. It backs up the mail Thunderbird has already saved, so open Thunderbird and let its mail arrive before a backup. MailArchive Desktop can't set up a scheduled backup of Thunderbird mail here; the MailArchive wizard can: when it asks Keep this archive current automatically?, choose one of the Yes answers.",
	}, not: []string{"Sync Thunderbird before each backup", "Stop waiting and back up now", "the Dock", "taskbar", "The Thunderbird sync looks for it", "If it was asleep", "the Thunderbird sync, if you turn it on", "Sync Thunderbird before each scheduled backup?"}},
}

// covers: MA-945, X11, X16, R12, S41
// The guide's words, pinned whole, per platform, each true of this version's code: the
// sync's sentences only where the sync runs (macOS and Windows), Run now's words from
// the same table Run now itself reads (presync.AttendedStartAllowed) — on a Mac it
// starts Thunderbird, on Windows it does not yet — and Linux told plainly that there is
// no sync. The Microsoft 365 steps name the permissions the code asks for (delegated
// Mail.Read and offline_access, internal/graph/delegated.go) and the sign-in the
// Desktop's Archive now and scheduled backups always use (device, capture.go).
func TestHowToUseMailArchiveSaysWhatThisVersionDoes(t *testing.T) {
	for _, p := range helpPlatforms {
		page := getOverview(t, Config{GOOS: p.goos, ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
		h := readHelpCard(t, page)
		assure.Reached(t, h.text, p.goos+": the guide's text")
		for _, s := range append(append([]string{}, helpSentences...), p.want...) {
			if !strings.Contains(h.text, s) {
				t.Errorf("%s: the guide lacks %q", p.goos, s)
			}
		}
		for _, s := range p.not {
			if strings.Contains(h.text, s) {
				t.Errorf("%s: the guide says %q, which is not true here", p.goos, s)
			}
		}
		for _, bad := range append([]string{"this PC", "Mail.ReadWrite", "Mail.Send", "posture", "mailarchive setup"}, helpGone...) {
			if strings.Contains(h.text, bad) {
				t.Errorf("%s: the guide says %q", p.goos, bad)
			}
		}
		// The "No archive chosen yet" card opens the wizard in the guide's own words for
		// this platform, never with a typed command a Mac or Windows app user does not have
		// (friction review M3).
		how := map[string]string{"darwin": helpMacWizard, "windows": helpWinWizard, "linux": helpLinWizard}[p.goos]
		if want := "To make a new one from Thunderbird, Outlook or other mail files on this computer, use the MailArchive wizard, then open its folder here. " + how; !strings.Contains(h.outside, want) {
			t.Errorf("%s: the no-archive card does not say %q:\n%s", p.goos, want, h.outside)
		}
		if strings.Contains(h.outside, "use the MailArchive wizard (mailarchive wizard)") {
			t.Errorf("%s: the no-archive card still names the wizard by its command", p.goos)
		}
	}

	// Run now's words follow the table Run now reads: switched the other way, the Mac
	// says it does not start Thunderbird yet and Windows says it does, in its own words.
	restore := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": false, "windows": true})
	defer restore()
	mac := readHelpCard(t, getOverview(t, Config{GOOS: "darwin", ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")}))
	if want := "Run now: on a Mac, Run now does not start Thunderbird yet."; !strings.Contains(mac.text, want) || strings.Contains(mac.text, helpMacRunNow) || strings.Contains(mac.text, helpEncSync) {
		t.Errorf("the Mac guide with Run now's start off does not say %q, or still promises the sync:\n%s", want, mac.text)
	}
	win := readHelpCard(t, getOverview(t, Config{GOOS: "windows", ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")}))
	for _, want := range []string{"It starts Thunderbird minimized to the taskbar and waits for its mail (up to 20 minutes, often about 13).", "When the backup is done, Thunderbird stays open: quit it from the taskbar or the notification area."} {
		if !strings.Contains(win.text, want) {
			t.Errorf("the Windows guide with Run now's start on lacks %q", want)
		}
	}
	if strings.Contains(win.text, "does not start Thunderbird yet") {
		t.Error("the Windows guide with Run now's start on still says Run now does not start Thunderbird")
	}
	// Run now's wait in Getting started follows the same table.
	const wait = "it first waits for Thunderbird's mail"
	if strings.Contains(mac.text, wait) || !strings.Contains(win.text, wait) {
		t.Errorf("Getting started's Run now wait does not follow the start table (Mac off: %v, Windows on: %v)", strings.Contains(mac.text, wait), strings.Contains(win.text, wait))
	}
}

// m365LocalOnlyLine is the Microsoft 365 connection card's line on an archive made from mail
// files (as HTML), pinned whole: the guide's own steps, never a terminal command.
const m365LocalOnlyLine = `Not used by this archive: it was made from mail files on this computer. To back up a Microsoft 365 mailbox, keep it in an archive of its own: in <b>Settings → Archive location</b>, type a new folder and click <b>Save settings</b>; this card then asks for the Microsoft 365 details.`

// covers: MA-945, MA-938, X11
// The Microsoft 365 connection card and the guide give one way to add a Microsoft 365
// mailbox to a computer whose current archive was made from mail files (friction review
// M2: the card said to run mailarchive setup in a terminal, the guide said to use Settings).
// The way is driven: on a local archive the card shows the line and no form; Save settings
// with a new folder in Archive location is answered ok, and the card then shows the
// Microsoft 365 form and no longer the line. Positive twin first: a Microsoft 365 archive's
// card shows the form.
func TestTheMicrosoft365CardAndTheGuideAgree(t *testing.T) {
	dir := t.TempDir()
	none := filepath.Join(dir, "none.json")
	m365 := getOverview(t, Config{GOOS: "darwin", ConfigPath: none, SettingsPath: filepath.Join(t.TempDir(), "s.json"),
		Out: archiveAt(t, dir, "M365", map[string]string{"mailbox:u@contoso.com": "b"})})
	if !strings.Contains(m365, `<div id="m365Form">`) || strings.Contains(m365, "Not used by this archive") {
		t.Fatal("the positive twin: a Microsoft 365 archive's card has no form, or says it is not used")
	}
	// The friction review's O3 (applied by the lead, 2026-10-31): under the App-only choice the
	// card says the Desktop never uses it. True of the code: Archive now and the scheduled
	// capture both run with Auth "device" (capture.go), and the sign-in is graph.NewDelegated
	// (signin.go); App-only is read only by the mailarchive command line.
	form := m365[strings.Index(m365, `<div id="m365Form">`):]
	form = form[:strings.Index(form, `<div id="m365SecretRow"`)]
	if want := `App-only (admin): a client secret, admin-consented mailboxes</label>` + "\n        " + `<p class="card-sub" style="margin-top:4px">MailArchive Desktop always signs in as you; App-only is for the command line.</p>`; !strings.Contains(form, want) {
		t.Errorf("the connection card does not say under App-only that the Desktop always signs in as you:\n%s", form)
	}
	for _, src := range []string{"capture.go", "signin.go"} {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `Auth: "app"`) || (src == "capture.go" && strings.Count(string(b), `Auth: "device"`) != 2) {
			t.Errorf("%s no longer signs in only as you, so the App-only line is untrue", src)
		}
	}

	local := archiveAt(t, dir, "Local", map[string]string{"path:/mail/Local Folders": "b"})
	h := DashboardHandler(Config{GOOS: "darwin", ConfigPath: none, SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: local})
	page, csrf := getPage(t, h)
	if !strings.Contains(page, `<div class="v">`+m365LocalOnlyLine+`</div>`) || strings.Contains(page, `id="m365Form"`) {
		t.Fatalf("a local archive's Microsoft 365 card does not give the guide's steps:\n%s", page)
	}
	for _, bad := range []string{"run <span class=\"mono\">mailarchive setup</span>", "in a terminal.</div>"} {
		if strings.Contains(page, bad) {
			t.Errorf("the card still says %q", bad)
		}
	}
	guide := readHelpCard(t, page).text
	if want := "If the card says Microsoft 365 is not used by this archive, the current archive was made from mail files. Keep the mailbox in an archive of its own: in Settings → Archive location, type a new folder and click Save settings"; !strings.Contains(guide, want) {
		t.Errorf("the guide no longer says %q", want)
	}

	// The step itself: a new folder in Archive location, Save settings.
	fresh := filepath.Join(t.TempDir(), "Mailbox archive")
	if body := postJSON(h, "/api/settings", csrf, `{"Out":`+jsonQuote(fresh)+`}`).Body.String(); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("Save settings with a new folder: %s", body)
	}
	after, _ := getPage(t, h)
	if !strings.Contains(after, `<div id="m365Form">`) || strings.Contains(after, "Not used by this archive") {
		t.Error("after Save settings with a new folder, the card does not ask for the Microsoft 365 details")
	}
}

// covers: MA-945, X11
// The guide names only controls that exist: every button, box and field it tells a
// person to click or fill in is on the Overview with no archive (outside the guide), or,
// for one drawn only in another state, in the source that draws it — Run now, Repair and
// Remove on the Scheduled backup card, the Thunderbird sync boxes, the Encryption card's
// Unlock and Lock, Clear sign-in, the move question's "Save and keep the backup", and the
// wizard's own words.
func TestHowToUseMailArchiveNamesRealControls(t *testing.T) {
	page := getOverview(t, Config{GOOS: "darwin", ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	h := readHelpCard(t, page)
	assure.Reached(t, h.outside, "the page outside the guide")
	for _, label := range []string{"Open an archive folder…", "Make a new encrypted archive…", "Use this archive", "Archive location", "How often",
		"Save settings", "Directory (tenant) ID", "Application (client) ID", "Device (per-user)", "App-only (admin)", "Save configuration",
		"Archive now", "Keep each message's original .eml too", "Include Deleted Items", "Include Junk Email", "Save alert settings", "Send test email",
		"Shut down", "Stop waiting and back up now", "Cancel the backup", "Recent backups", "Status & health", "Microsoft 365 connection", "Settings",
		"Send backup alerts by email", "a failed backup, or Thunderbird not synced for 3 days", "I have saved the recovery key"} {
		if !strings.Contains(h.text, label) {
			t.Errorf("fixture: the guide does not name %q", label)
		}
		if !strings.Contains(h.outside, label) {
			t.Errorf("the guide names %q, which is not on the page", label)
		}
	}
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	dash, wiz := read("dashboard.go"), read(filepath.Join("..", "wizard", "wizard.go"))
	wxs := read(filepath.Join("..", "..", "packaging", "mailarchive-desktop", "Product.wxs"))
	for _, c := range []struct{ label, src, where string }{
		{`>Run now</button>`, dash, "dashboard.go"},
		{`>Repair</button>`, dash, "dashboard.go"},
		{`>Remove</button>`, dash, "dashboard.go"},
		{`>Unlock</button>`, dash, "dashboard.go"},
		{`>Lock</button>`, dash, "dashboard.go"},
		{`>Clear sign-in</button>`, dash, "dashboard.go"},
		{`>Add another archive…</button>`, dash, "dashboard.go"},
		{`>Switch to this archive</button>`, dash, "dashboard.go"},
		{`>Pause (make read-only)</button>`, dash, "dashboard.go"},
		{`>Resume</button>`, dash, "dashboard.go"},
		{`Current archive</span>`, dash, "dashboard.go"},
		{`> Sync Thunderbird before each backup</label>`, dash, "dashboard.go"},
		{`>Save and keep the backup</button>`, dash, "dashboard.go"},
		{`"Archive a mail file on this computer (Outlook, Thunderbird, mbox)"`, wiz, "wizard.go"},
		{`"Keep this archive current automatically?`, wiz, "wizard.go"},
		{`"Yes — every day at 2:00 AM"`, wiz, "wizard.go"},
		{`"Sync Thunderbird before each scheduled backup?`, wiz, "wizard.go"},
		{`"Yes — sync Thunderbird first"`, wiz, "wizard.go"},
		{`Prepare a full offline download first?"`, wiz, "wizard.go"},
		{`zenity.OKLabel("Yes, prepare")`, wiz, "wizard.go"},
		{`"Microsoft 365, folder export, browsing & tools (open MailArchive Desktop)"`, wiz, "wizard.go"},
		{`id="encTypeBack"`, dash, "dashboard.go"},
		{`id="encCreateRemember" checked`, dash, "dashboard.go"},
		{`Name="MailArchive Desktop"`, wxs, "Product.wxs"},
		{`Name="MailArchive wizard"`, wxs, "Product.wxs"},
	} {
		if !strings.Contains(c.src, c.label) {
			t.Errorf("the guide names %s, which %s does not draw", c.label, c.where)
		}
	}
	if encSyncLabel != "Sync Thunderbird before each Run now" {
		t.Errorf("the encrypted sync box is %q, which the guide does not name", encSyncLabel)
	}
}
