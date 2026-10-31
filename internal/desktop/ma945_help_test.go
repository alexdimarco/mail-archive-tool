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
	wantParts := []string{"Getting started", "Thunderbird", "Microsoft 365"}
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
			noArchive := cfg.Out == ""
			if got := strings.Contains(h.outside, pointer); got != noArchive {
				t.Errorf("%s: the Overview's pointer to the guide shown = %v, want %v", what, got, noArchive)
			}
			if noArchive && !strings.Contains(page, `<a href="#sec-help">How to use MailArchive</a> below`) {
				t.Errorf("%s: the no-archive pointer is not a link to the guide", what)
			}
			if h.open["Getting started"] != noArchive || h.open["Thunderbird"] || h.open["Microsoft 365"] {
				t.Errorf("%s: open parts %v, want Getting started open only with no archive", what, h.open)
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
	"MailArchive only reads your mail: it never sends, moves, changes or deletes it. The one exception is the Thunderbird sync, if you turn it on (see Thunderbird below).",
	"Then come back here and click Use this archive if this page offers it, or click Open an archive folder… and choose the archive's folder.",
	"An archive you already have (from another computer, a drive or a backup copy): click Open an archive folder… and choose its folder.",
	"Run now backs up straight away, Repair fixes a scheduled backup that stopped working, and Remove deletes the scheduled backup (the archive and its mail are kept). A Microsoft 365 archive uses Archive now instead of Run now.",
	"Go back in time shows your mailbox as it was on an earlier date; only a Microsoft 365 archive keeps that history, from its first backup.",
	"Status & health: whether this archive is healthy. When something needs attention, it says what is wrong and what to do.",
	"Fill in your mail server, click Save alert settings, then Send test email to check that it arrives. An encrypted archive has no Backup alerts card.",
	"Pause (make read-only) stops all changes to an archive, and Resume allows them again. Nothing is deleted either way.",
	"MailArchive then shows a recovery key once: save it somewhere safe, such as a password manager. If you lose both the password and the recovery key, nobody can open the archive, not even you.",
	"Scheduled backups of an encrypted archive aren't available yet: back it up with Run now while it is unlocked.",
	"Shut down (at the top of the page): stops MailArchive Desktop. Your archives and scheduled backups are not affected; open MailArchive again to come back.",
	// Thunderbird
	"Your mail account added to Thunderbird, and Thunderbird opened at least once, so it has downloaded your mail.",
	"For an IMAP account this is the setting Keep messages for this account on this computer: in Thunderbird, open Account Settings (from the ≡ menu at the top right), choose your account's Synchronization & Storage page, and check that the box is ticked. Thunderbird ticks it for a new account. POP accounts and Local Folders always keep their mail on this computer.",
	"A message Thunderbird has not downloaded yet is archived as still missing its content, and a later backup fills it in once Thunderbird has downloaded it.",
	"MailArchive uses the Thunderbird profile that holds the mail folder this archive was made from",
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
	"Sync Thunderbird before each backup. Thunderbird saves new mail only while it is running. With this setting on, each backup first starts Thunderbird and waits for its mail, so the archive also gets mail that had only reached the mail server. It is off until you turn it on: in Settings, choose How often, tick Sync Thunderbird before each backup, then click Save settings.",
	"The backup waits for its mail to stop arriving (up to 20 minutes), asks Thunderbird to quit, then backs up. A scheduled backup starts Thunderbird only when no one has used the keyboard or mouse for 2 minutes. If you are using the computer, that backup skips the sync and saves the mail already on this computer.",
	"A Thunderbird you already have open is left alone: MailArchive never starts or closes it, and the backup reads the mail it has saved.",
	"It checks all your accounts, sends anything waiting in your Outbox, runs your message filters, and may show notifications. When a scheduled backup asks it to quit, your accounts' on-exit actions run too (for example emptying Trash), so mail you deleted since the last backup may be removed before it is archived.",
	"With Hourly and the Thunderbird sync, Thunderbird stays open for about 20 minutes in an hour with no new mail, while the backup waits for mail.",
	"When the sync keeps missing: if Thunderbird could not be synced before this archive's scheduled backups for 3 days, Status & health says so and what to do, and Backup alerts sends one email (when a failed backup, or Thunderbird not synced for 3 days is ticked).",
	"A softer warning, with no email, shows when Thunderbird was started before each scheduled backup for 3 days but no new mail arrived. Thunderbird may be waiting for a password or a sign-in: open Thunderbird and answer it. That warning clears by itself the next time a backup sees new mail arrive.",
}

const (
	helpMacRunNow = "Run now syncs Thunderbird too, even while you are using the computer. It starts Thunderbird out of sight and waits for its mail (up to 20 minutes, often about 13). Click Stop waiting and back up now to back up the mail that has arrived so far. Cancel the backup skips the whole backup instead, and nothing is added to the archive. When the backup is done, Thunderbird stays open: quit it from the Dock."
	helpWinNoRunNow = "Run now: on Windows, Run now does not start Thunderbird yet. It backs up the mail already on this computer. To include the newest mail, open Thunderbird, let its mail arrive, then click Run now. Scheduled backups still sync Thunderbird first."
	helpEncSync     = "An encrypted archive is backed up only when you click Run now. If it was made from Thunderbird, its Settings card offers Sync Thunderbird before each Run now, with its own Save. Until that backup starts reading mail, Lock cancels it."
)

var helpPlatforms = []helpPlatform{
	{goos: "darwin", want: append([]string{
		"On a Mac, open Mail Archive.app and choose Archive a mail file on this computer (Outlook, Thunderbird, mbox).",
		"Thunderbird installed in the Applications folder, or in the Applications folder inside your home folder. The Thunderbird sync looks for it only there.",
		"(Thunderbird keeps its profiles in ~/Library/Thunderbird/Profiles.)",
		"Scheduled backups: Thunderbird starts out of sight.",
		helpMacRunNow, helpEncSync,
		"On a Mac, the first time a scheduled backup closes Thunderbird, macOS may ask for permission to control Thunderbird; until it is answered, each run leaves Thunderbird open. To answer, open System Settings > Privacy & Security > Automation and allow the entry listed there to control Thunderbird.",
	}, helpSyncSentences...), not: []string{"does not start Thunderbird yet", "taskbar", "MailArchive does not sync Thunderbird"}},
	{goos: "windows", want: append([]string{
		"On Windows, open MailArchive wizard from the Start menu. If MailArchive was unzipped rather than installed, there is no Start menu entry: open a Command Prompt in the MailArchive folder and type mailarchivew.exe wizard.",
		`Thunderbird installed in its usual place: a Mozilla Thunderbird folder in Program Files (or Program Files (x86)), or in AppData\Local\Programs in your user folder. The Thunderbird sync looks for it only there.`,
		`(Thunderbird keeps its profiles in %APPDATA%\Thunderbird\Profiles.)`,
		"Scheduled backups: Thunderbird's window shows for a moment, then it is minimized to the taskbar.",
		helpWinNoRunNow,
	}, helpSyncSentences...), not: []string{"Stop waiting and back up now to back up", "the Dock", "System Settings", "Sync Thunderbird before each Run now", "MailArchive does not sync Thunderbird"}},
	{goos: "linux", want: []string{
		"On Linux, run mailarchive wizard.",
		"(Thunderbird keeps its profiles in ~/.thunderbird.)",
		"On Linux, MailArchive does not sync Thunderbird. It backs up the mail Thunderbird has already saved, so open Thunderbird and let its mail arrive before a backup. MailArchive Desktop can't set up a scheduled backup of Thunderbird mail here; the MailArchive wizard can: when it asks Keep this archive current automatically?, choose one of the Yes answers.",
	}, not: []string{"Sync Thunderbird before each backup", "Stop waiting and back up now", "the Dock", "taskbar", "The Thunderbird sync looks for it"}},
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
		for _, bad := range []string{"this PC", "Mail.ReadWrite", "Mail.Send", "posture", "mailarchive setup"} {
			if strings.Contains(h.text, bad) {
				t.Errorf("%s: the guide says %q", p.goos, bad)
			}
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
		"Shut down", "Stop waiting and back up now", "Cancel the backup", "Recent backups", "Status & health", "Microsoft 365 connection", "Settings"} {
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
	} {
		if !strings.Contains(c.src, c.label) {
			t.Errorf("the guide names %s, which %s does not draw", c.label, c.where)
		}
	}
	if encSyncLabel != "Sync Thunderbird before each Run now" {
		t.Errorf("the encrypted sync box is %q, which the guide does not name", encSyncLabel)
	}
}
