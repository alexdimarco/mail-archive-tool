package desktop

import (
	"html/template"
	"strings"

	"mail-archive-tool/internal/presync"
)

// The "How to use MailArchive" guide (MA-945; owner request of 2026-10-31, shipped in
// 1.5.2): a card on the Overview that the sidebar's first entry jumps to on every page,
// in every archive state. It is a card on the Overview, not a page of its own, so the
// Overview's sidebar keeps to same-page anchors (MA-302) and the guide needs no route.
// Its words are this version's truth, per platform: what differs between the Mac,
// Windows and Linux is drawn from the same table and constants the code itself reads
// (presync.AttendedStartAllowed for Run now, runNowWordsFor, syncWaitWords,
// presync.MacAutomationNote, presync.HourlySyncNote), so the guide cannot promise what
// Run now does not do. It loads nothing and runs no script: static text and same-page
// links only. Its friction review (docs/review-help-page-friction.md) and a truth review
// of 2026-10-31 are applied; what they held for the owner is listed in that review.

// helpData is what the guide is drawn from: this computer's platform, whether Getting
// started opens by itself (no archive chosen yet), and whether the Backup alerts card is on
// the page to link to (every archive but an encrypted one).
type helpData struct {
	Open bool
	// Alerts: the page draws the Backup alerts card (#sec-alerts), so the guide links to it.
	Alerts bool
	// On is how a sentence names the platform: "on a Mac", "on Windows", "on Linux".
	On string
	// Sync: the Thunderbird sync runs on this platform (macOS and Windows; never Linux).
	Sync bool
	// RunNowSyncs: Run now starts Thunderbird here (runNowSyncs, the AttendedStartAllowed table).
	RunNowSyncs bool
	// Hidden and Place: how a Thunderbird Run now started is left, and where it is quit from.
	Hidden, Place string
	// Wait is how long Run now's wait for mail takes (syncWaitWords).
	Wait string
	// WizardHow is how to open the MailArchive wizard here.
	WizardHow string
	// AppPlace is where the sync looks for Thunderbird ("" where there is no sync).
	AppPlace string
	// Profiles is the folder Thunderbird keeps its profiles in.
	Profiles string
	// SchedStart is how a scheduled backup starts Thunderbird here.
	SchedStart string
	// MacNote is macOS's Automation question (presync.MacAutomationNote); "" elsewhere.
	MacNote string
	// Hourly is the note on Hourly with the sync (presync.HourlySyncNote).
	Hourly string
	// Reload is how to reload this page here ("press Command-R").
	Reload string
	// Missed is what happens to a scheduled time the computer missed: the Mac's launchd
	// (StartCalendarInterval, launchd.plist(5)) and the Windows task (StartWhenAvailable)
	// run it on waking; Linux's cron skips it.
	Missed string
	// ComeBack is how to open MailArchive Desktop again after Shut down.
	ComeBack string
}

// helpMissedOnWake is Missed where the scheduler catches up a time slept through.
const helpMissedOnWake = "A time when the computer is switched off is skipped. If it was asleep, the backup runs when it wakes up."

// helpDataFor is the guide's data on goos.
func helpDataFor(goos string, open bool) helpData {
	w := runNowWordsFor(goos)
	d := helpData{Open: open, RunNowSyncs: runNowSyncs(goos), Hidden: w.hidden, Place: w.place, Wait: syncWaitWords, Hourly: presync.HourlySyncNote}
	switch goos {
	case "darwin":
		d.On, d.Sync = "on a Mac", true
		d.WizardHow = "On a Mac, open Mail Archive.app and choose Archive a mail file on this computer (Outlook, Thunderbird, mbox)."
		d.AppPlace = " in the Applications folder, or in the Applications folder inside your home folder. The Thunderbird sync looks for it only there"
		d.Profiles = "~/Library/Thunderbird/Profiles"
		d.SchedStart = "Thunderbird starts out of sight."
		d.MacNote = presync.MacAutomationNote
		d.Reload, d.Missed = "press Command-R", helpMissedOnWake
		// A double-click on Mail Archive.app opens the wizard (noarg M1); the Desktop is its
		// surfaceDesktopItem.
		d.ComeBack = "To come back, open Mail Archive.app and choose Microsoft 365, folder export, browsing & tools (open MailArchive Desktop)."
	case "windows":
		d.On, d.Sync = "on Windows", true
		d.WizardHow = "On Windows, open MailArchive wizard from the Start menu. If MailArchive was unzipped rather than installed, there is no Start menu entry: open the MailArchive folder, click in the address bar at the top of the window, type cmd and press Enter, then type mailarchivew.exe wizard and press Enter."
		d.AppPlace = ` in its usual place: a Mozilla Thunderbird folder in Program Files (or Program Files (x86)), or in AppData\Local\Programs in your user folder. The Thunderbird sync looks for it only there`
		d.Profiles = `%APPDATA%\Thunderbird\Profiles`
		d.SchedStart = "Thunderbird's window shows for a moment, then it is minimized to the taskbar."
		d.Reload, d.Missed = "press F5", helpMissedOnWake
		// The installer's Start menu shortcut (Product.wxs); mailarchivew.exe opened with no
		// console starts the Desktop (noarg W3).
		d.ComeBack = "To come back, open MailArchive Desktop from the Start menu, or, if MailArchive was unzipped, double-click mailarchivew.exe in its folder."
	default:
		d.On = "on Linux"
		d.WizardHow = "On Linux, run mailarchive wizard."
		d.Profiles = "~/.thunderbird"
		d.Reload = "press F5"
		d.Missed = "A time when the computer is off or asleep is skipped." // cron
		d.ComeBack = "To come back, run mailarchive desktop."
	}
	return d
}

// helpCard is the guide's card, drawn for goos; open opens Getting started, and alerts says
// the page has a Backup alerts card to link to.
func helpCard(goos string, open, alerts bool) template.HTML {
	var b strings.Builder
	d := helpDataFor(goos, open)
	d.Alerts = alerts
	if err := helpTmpl.Execute(&b, d); err != nil {
		return template.HTML(`<div class="card help" id="sec-help"><h2>How to use MailArchive</h2><p class="card-sub">The guide could not be shown.</p></div>`)
	}
	return template.HTML(b.String())
}

// helpUpper is s with its first letter in upper case, for a sentence that starts with a
// platform phrase ("On a Mac, ...").
func helpUpper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

var helpTmpl = template.Must(template.New("help").Funcs(template.FuncMap{"upper": helpUpper}).Parse(`<div class="card help" id="sec-help">
      <h2>How to use MailArchive</h2>
      <p class="card-sub">A short guide to this page. Open a part to read it.</p>
      <details id="helpStart"{{if .Open}} open{{end}}><summary>Getting started</summary>
        <p>MailArchive keeps a copy of your mail in an archive folder on this computer. Each backup adds the mail that is new, and you can read and search the archive in your browser. MailArchive only reads your mail: it never sends, moves, changes or deletes it. {{if .Sync}}The exceptions are the Thunderbird sync, if you turn it on, and the wizard's offer to turn on Thunderbird's offline download{{else}}The one exception is the wizard's offer to turn on Thunderbird's offline download{{end}} (see Thunderbird below).</p>
        <p><b>Your first archive.</b> Choose the line that fits your mail:</p>
        <ul>
          <li><b>Thunderbird, Outlook or other mail files on this computer:</b> make the archive with the MailArchive wizard. {{.WizardHow}} Choose your mail, then a new folder for the archive. When the wizard asks <b>Keep this archive current automatically?</b>, choose one of the Yes answers to back it up on a schedule.{{if .Sync}} For Thunderbird mail the wizard then asks <b>Sync Thunderbird before each scheduled backup?</b> (see Thunderbird below). You can also set <b>How often</b> and the sync later in Settings on this page.{{end}} Then come back to this page and reload it ({{.Reload}}). Click <b>Use this archive</b> if the page offers it, or click <b>Open an archive folder…</b> and choose the archive's folder.</li>
          <li><b>A Microsoft 365 mailbox:</b> follow the steps under Microsoft 365 below.</li>
          <li><b>An archive you already have</b> (from another computer, a drive or a backup copy): click <b>Open an archive folder…</b> and choose its folder.</li>
        </ul>
        <p><b>The parts of this page</b>, in the order you will need them:</p>
        <ul>
          <li><a href="#sec-settings">Settings</a>: <b>Archive location</b> is the folder of the archive this page shows. <b>How often</b> and <b>At</b>, when Settings shows them, set when the scheduled backup runs.{{if .Sync}} For an archive made from Thunderbird, <b>Sync Thunderbird before each backup</b> is here too.{{end}} Click <b>Save settings</b> to keep a change.</li>
          <li><a href="#sec-backup">Scheduled backup</a> and <b>Run now</b>: a scheduled backup runs by itself at its time, even when this page is closed, as long as you are logged in to this computer. {{.Missed}} The Scheduled backup card shows what is set up. <b>Run now</b> starts a backup now{{if .RunNowSyncs}} (with the Thunderbird sync on, it first waits for Thunderbird's mail; see Thunderbird below){{end}}, <b>Repair</b> fixes a scheduled backup that stopped working, and <b>Remove</b> deletes the scheduled backup (the archive and its mail are kept). A Microsoft 365 archive uses <a href="#sec-archive">Archive now</a> instead of Run now.</li>
          <li><b>Open archive</b> and <b>Go back in time</b> (in the sidebar): Open archive shows the archive in a new browser tab, where you can read and search your mail. Go back in time shows your mailbox as it was on an earlier date; only a Microsoft 365 archive keeps that history, from its first backup.</li>
          <li><a href="#sec-history">Recent backups</a>: this archive's last backups, newest first, and how each one ended.</li>
          <li><a href="#sec-health">Status &amp; health</a>: whether this archive is healthy. When something needs attention, it says what is wrong and what to do. Look at it after your first backup, and whenever an alert email arrives.</li>
          <li>{{if .Alerts}}<a href="#sec-alerts">Backup alerts</a>{{else}}<b>Backup alerts</b>{{end}}: email you when a scheduled backup fails,{{if .Sync}} when Thunderbird has not been synced for 3 days,{{end}} or when backups stopped because the archive's mail source could not be reached for too long. Tick <b>Send backup alerts by email</b>, then tick what you want to hear about (start with <b>a failed backup, or Thunderbird not synced for 3 days</b>). Fill in your mail server, click <b>Save alert settings</b>, then <b>Send test email</b> to check that it arrives. The test email is sent even when alerts are switched off, so check the ticks too. An encrypted archive has no Backup alerts card.</li>
          <li><b>Your archives</b>: shown when MailArchive Desktop knows more than one archive. The one marked <b>Current archive</b> is the one this page shows and changes. <b>Switch to this archive</b> makes another one current. <b>Pause (make read-only)</b> stops all changes to an archive, and <b>Resume</b> allows them again. Nothing is deleted either way.</li>
          <li><b>Add another archive</b>: <b>Add another archive…</b> opens the wizard's windows to make a second archive, for example from another Thunderbird profile or an Outlook .pst file. Your current archive is not changed. <b>Open an archive folder…</b> opens one you already have.</li>
          <li><b>Encrypted archives</b>: an encrypted archive opens only with its password or its recovery key. To make one, click <b>Make a new encrypted archive…</b>. The wizard's windows ask what to back up and where, and you type the password on this page. MailArchive then shows a recovery key: save it somewhere safe, such as a password manager, click <b>I have saved the recovery key</b>, and type it back to show it was saved. If you lose both the password and the recovery key, only a computer that remembers the archive's key can still open it; otherwise nobody can, not even you. Its Encryption card has <b>Unlock</b> and <b>Lock</b>. Scheduled backups of an encrypted archive aren't available yet: back it up with <b>Run now</b> while it is unlocked. An encrypted archive made at the command line has no Run now: it is backed up at the command line, and its Archive now card says how.</li>
          <li><b>Shut down</b> (at the top of the page): stops MailArchive Desktop. Your archives and scheduled backups are not affected. {{.ComeBack}}</li>
        </ul>
      </details>
      <details id="helpThunderbird"><summary>Thunderbird</summary>
        <p><b>What MailArchive needs from Thunderbird:</b></p>
        <ol>
          <li>Thunderbird installed{{.AppPlace}}.</li>
          <li>Your mail account added to Thunderbird, and Thunderbird opened at least once, so it has downloaded your mail.</li>
          <li>Thunderbird keeping a copy of each message on this computer. MailArchive backs up the mail Thunderbird has saved here. For an IMAP account this is the setting <b>Keep messages for this account on this computer</b>. Thunderbird turns it on for a new account, so usually there is nothing to change. To check it, in Thunderbird open <b>Account Settings</b> (from the ≡ menu at the top right), choose your account's <b>Synchronization &amp; Storage</b> page, and see that the box is ticked. POP accounts and Local Folders always keep their mail on this computer. Downloading takes time: Thunderbird saves your messages to this computer over time, and only while it is open. A message Thunderbird has not downloaded is not in the archive yet; a later backup adds it once Thunderbird has downloaded it. A message Thunderbird saved only in part (for example without a large attachment) is archived with what is there, and a later backup fills in the rest. If you make the archive from an IMAP account, the MailArchive wizard may ask <b>Prepare a full offline download first?</b> <b>Yes, prepare</b> makes sure this setting is on (it asks you to quit Thunderbird if it is open, and keeps a copy of Thunderbird's settings file beside it), then waits while you open Thunderbird and it downloads your mail.</li>
          <li>The right profile. MailArchive uses the Thunderbird profile that holds the mail folder this archive was made from. (Thunderbird keeps its profiles in {{.Profiles}}.) If you use more than one Thunderbird profile, only that one is backed up and synced.</li>
        </ol>
        {{if .Sync}}<p><b>Sync Thunderbird before each backup.</b> Thunderbird saves new mail only while it is running. With this setting on, each backup first starts Thunderbird and waits for its mail, so the archive also gets mail that had only reached the mail server. It is off until you turn it on: choose <b>Yes — sync Thunderbird first</b> when the MailArchive wizard asks <b>Sync Thunderbird before each scheduled backup?</b>, or, in <a href="#sec-settings">Settings</a>, choose <b>How often</b>, tick <b>Sync Thunderbird before each backup</b>, then click <b>Save settings</b>.</p>
        <ul>
          <li><b>Scheduled backups:</b> {{.SchedStart}} The backup waits for its mail to stop arriving (up to 20 minutes), asks Thunderbird to quit, then backs up. A scheduled backup starts Thunderbird only when no one has used the keyboard or mouse for 2 minutes. If you are using the computer, that backup skips the sync and saves the mail already on this computer.</li>
          {{if .RunNowSyncs}}<li><b>Run now</b> syncs Thunderbird too, even while you are using the computer. It starts Thunderbird {{.Hidden}} and waits for its mail ({{.Wait}}). Click <b>Stop waiting and back up now</b> to back up the mail that has arrived so far. <b>Cancel the backup</b> skips the whole backup instead, and nothing is added to the archive. When the backup is done, Thunderbird stays open: quit it from {{.Place}}.</li>
          {{else}}<li><b>Run now</b>: {{.On}}, Run now does not start Thunderbird yet. It backs up the mail already on this computer. To include the newest mail, open Thunderbird, let its mail arrive, then click Run now. Scheduled backups still sync Thunderbird first.</li>
          {{end}}<li><b>A Thunderbird you already have open is left alone:</b> MailArchive never starts or closes it, and the backup reads the mail it has saved.</li>
          <li><b>What starting Thunderbird does:</b> the sync opens your real Thunderbird, so it does what opening Thunderbird always does. It checks all your accounts, sends anything waiting in your Outbox, runs your message filters, and may show notifications. When a scheduled backup asks it to quit, your accounts' on-exit actions run too (for example emptying Trash), so mail you deleted since the last backup may be removed before it is archived.</li>
          <li>{{.Hourly}}</li>
          {{if .MacNote}}<li><b>macOS permission:</b> {{.MacNote}}</li>
          {{end}}<li><b>When the sync keeps missing:</b> if Thunderbird could not be synced before this archive's scheduled backups for 3 days, <a href="#sec-health">Status &amp; health</a> says so and what to do, and Backup alerts sends one email, when <b>Send backup alerts by email</b> and <b>a failed backup, or Thunderbird not synced for 3 days</b> are both ticked. A softer warning, with no email, shows when Thunderbird was started before each scheduled backup for 3 days but no new mail arrived. Thunderbird may be waiting for a password or a sign-in: open Thunderbird and answer it. That warning clears by itself the next time a backup sees new mail arrive. <a href="#sec-history">Recent backups</a> also shows how each sync went.</li>
          {{if .RunNowSyncs}}<li><b>An encrypted archive</b> is backed up only when you click Run now. If it was made from Thunderbird, its Settings card offers <b>Sync Thunderbird before each Run now</b>, with its own <b>Save</b>. Until that backup starts reading mail, <b>Lock</b> cancels it.</li>
          {{end}}</ul>
        {{else}}<p><b>{{upper .On}}, MailArchive does not sync Thunderbird.</b> It backs up the mail Thunderbird has already saved, so open Thunderbird and let its mail arrive before a backup. MailArchive Desktop can't set up a scheduled backup of Thunderbird mail here; the MailArchive wizard can: when it asks <b>Keep this archive current automatically?</b>, choose one of the Yes answers.</p>
        {{end}}</details>
      <details id="helpM365"><summary>Microsoft 365</summary>
        <p>MailArchive Desktop backs up <b>your own</b> Microsoft 365 mailbox. You sign in as yourself with a code from Microsoft, and MailArchive only reads your mail: it asks Microsoft for read access (the Mail.Read permission) and nothing more. Shared mailboxes and other people's mailboxes aren't available in this version.</p>
        <p><b>Step 1: register MailArchive with your organization (once).</b> This is done in the Microsoft Entra admin center, by someone allowed to register apps for your organization's Microsoft 365. If that isn't you, send these steps to your IT person.</p>
        <ol>
          <li>Open <b>App registrations</b> and click <b>New registration</b>. Name it <b>mailarchive</b>. Under supported account types, choose <b>Accounts in this organizational directory only</b>. Leave the redirect URI empty.</li>
          <li>On the app's <b>Authentication</b> page, under <b>Advanced settings</b>, set <b>Allow public client flows</b> to <b>Yes</b>. MailArchive signs in with a code, so it needs no client secret.</li>
          <li>On the app's <b>API permissions</b> page, click <b>Add a permission</b>, choose <b>Microsoft Graph</b>, then <b>Delegated permissions</b>, and add <b>Mail.Read</b> and <b>offline_access</b>. offline_access lets later backups run without signing in again. MailArchive needs nothing else: it never asks to change or send mail.</li>
          <li>Most organizations let each person agree to these permissions the first time they sign in. If yours does not, an admin clicks <b>Grant admin consent</b> on the API permissions page, once.</li>
          <li>On the app's <b>Overview</b> page, copy two values: the <b>Directory (tenant) ID</b> and the <b>Application (client) ID</b>. Don't copy an <b>Object ID</b>: Object IDs look the same and sit right beside them, but they are the wrong values.</li>
        </ol>
        <p><b>Step 2: connect MailArchive Desktop.</b></p>
        <ol>
          <li>On the <a href="#sec-m365">Microsoft 365 connection</a> card, paste the Directory (tenant) ID into <b>Directory (tenant) ID</b> (your organization's domain, such as contoso.onmicrosoft.com, works too) and the Application (client) ID into <b>Application (client) ID</b>.</li>
          <li>Under <b>Authentication</b>, choose <b>Device (per-user)</b>, then click <b>Save configuration</b>.</li>
          <li>If no archive is chosen yet, type a new folder for the archive in <a href="#sec-settings">Settings</a> → <b>Archive location</b> and click <b>Save settings</b>.</li>
          <li>Click <a href="#sec-archive">Archive now</a>. The first time, the card shows a code and a link to a Microsoft page. Open the link, sign in as yourself and type the code. Before you approve, check that the page names the app mailarchive. Only type a code that this page has just shown you.</li>
          <li>Your sign-in is saved on this computer, so later backups don't ask again. If it stops working (for example after you change your password), click <b>Clear sign-in</b> on the Microsoft 365 connection card, then Archive now to sign in again.</li>
        </ol>
        <p><b>Step 3: keep it backed up.</b> In Settings, choose <b>How often</b> (Daily or Weekly) and the time, then click <b>Save settings</b>. The scheduled backup uses your saved sign-in and runs even when this page is closed, as long as you stay logged in to this computer. Settings also has three Microsoft 365 boxes: <b>Keep each message's original .eml too</b>, <b>Include Deleted Items</b> and <b>Include Junk Email</b>. They apply to every Microsoft 365 backup on this computer. Deleted Items and Junk Email are left out unless you tick them.</p>
        <p><b>If the card says Microsoft 365 is not used by this archive,</b> the current archive was made from mail files. Keep the mailbox in an archive of its own: in Settings → Archive location, type a new folder and click Save settings (if it asks about the scheduled backup, choose <b>Save and keep the backup</b>), then fill in the Microsoft 365 connection card.</p>
        <p><b>App-only (admin)</b>, the card's other choice, uses a client secret instead of your own sign-in. It is for an administrator who backs up other people's mailboxes with the mailarchive command line, and it needs the Mail.Read application permission with admin consent. MailArchive Desktop's Archive now and scheduled backups always sign in as you, so choose Device (per-user) here.</p>
      </details>
    </div>`))
