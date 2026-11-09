package alert

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// covers: MA-1188, MA-1184
// The still-downloading and stopped emails (slice C5, with the C5 friction review's P1 to P4 and
// lows, the lead's call of 2026-11-09), sent through the real SMTP transport to a loopback server:
// the alert settings decide whether they go (on, with the failure option, as the
// Thunderbird-not-synced email). The subject says what and "(backups OK)", never "after 3 days",
// and ends with the archive folder's own name; the body opens with "Your backups are working",
// then "Archive:" and "Computer:" lines (no Computer line when the name is unknown), the account
// lines, what to do (one line, or numbered steps), how to tell which account it is, and the
// once-only line. Every line is stripped of line breaks, so a crafted folder name cannot add a
// header. Positive twin first: the still-downloading email.
func TestTheThunderbirdDownloadEmails(t *testing.T) {
	on := Config{Enabled: true, OnFailure: true}
	if !on.ShouldSend(ResultTBBehind) {
		t.Fatal("positive twin: alerts on with the failure option do not send it")
	}
	for _, off := range []Config{{Enabled: false, OnFailure: true}, {Enabled: true, OnFailure: false, OnSuccess: true, OnPause: true}} {
		if off.ShouldSend(ResultTBBehind) {
			t.Errorf("%+v sends it", off)
		}
	}
	const which = `To see which account that is: in Thunderbird, click the menu button (≡, top right), then Account Settings; each account's "Server Settings" page shows the folder its mail is kept in, at the bottom.`

	addr, got := loopbackSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Enabled: true, OnFailure: true, Host: host, Port: p, Security: SecurityNone, From: "backup@example.org", To: "owner@example.org"}
	when := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)
	still := Event{Result: ResultTBBehind, Archive: "/Volumes/Backup/Mail", Host: "studio", When: when, TB: &TBBehind{Accounts: 1,
		Lines:  []string{"The account kept in the folder imap.gmail.com: about 120,000 of 238,080 messages downloaded, still going after 3 days."},
		Remedy: []string{"leave Thunderbird open (it downloads mostly while the computer is left alone). Later backups add the rest by themselves."},
		Which:  which,
	}}
	if err := Send(c, "", still); err != nil {
		t.Fatalf("positive twin: send: %v", err)
	}
	msg := <-got
	_, body, _ := strings.Cut(msg, "\r\n\r\n")
	if !strings.Contains(msg, "Subject: MailArchive: Thunderbird still downloading (backups OK) — Mail\r\n") {
		t.Errorf("the still-downloading subject:\n%s", msg)
	}
	wantStill := "Your backups are working. Thunderbird has not finished downloading one account's mail yet, so the backups have only part of it so far.\r\n\r\n" +
		"Archive: /Volumes/Backup/Mail\r\n" +
		"Computer: studio\r\n\r\n" +
		"The account kept in the folder imap.gmail.com: about 120,000 of 238,080 messages downloaded, still going after 3 days.\r\n\r\n" +
		"Do this on that computer: leave Thunderbird open (it downloads mostly while the computer is left alone). Later backups add the rest by themselves.\r\n\r\n" +
		which + "\r\n\r\n" +
		"You will get this email once. If the download then stops, you will get one more.\r\n\r\n" +
		"This message was sent by MailArchive's built-in backup alerts.\r\n"
	if body != wantStill {
		t.Errorf("the still-downloading body:\n%q\nwant\n%q", body, wantStill)
	}

	steps := []string{"1. Open Thunderbird and leave it open, with the computer left alone, for an hour or more. If it asks for a password, enter it.",
		"2. If Status & health in MailArchive Desktop still shows the same count the next day: in Thunderbird, click the menu button (≡, top right), then Account Settings. On that account's \"Synchronization & Storage\" page, make sure it is set to keep all of the account's mail on this computer."}
	stopped := Event{Result: ResultTBBehind, Archive: `C:\Users\a\OneDrive - DiMarco Technologies Inc\Mail Backups\Thunderbird archive 2026\`, When: when, TB: &TBBehind{Stopped: true, Accounts: 1,
		Lines:  []string{"The account kept in the folder imap.work.example: about 1,670 of 56,611 messages downloaded, no progress since Nov 4.", "The account kept in the folder imap.gmail.com: about 120,000 of 238,080 messages downloaded, still going after 5 days."},
		Remedy: steps, Which: which,
	}}
	subject, body := c.subjectAndBody(stopped)
	if subject != "MailArchive: Thunderbird stopped downloading (backups OK) — Thunderbird archive 2026" {
		t.Errorf("the stopped subject: %q", subject)
	}
	wantStopped := "Your backups are working, but Thunderbird has stopped downloading one account's mail, so the backups cannot get the rest until it starts again.\r\n\r\n" +
		"Archive: C:\\Users\\a\\OneDrive - DiMarco Technologies Inc\\Mail Backups\\Thunderbird archive 2026\\\r\n\r\n" +
		stopped.TB.Lines[0] + "\r\n" + stopped.TB.Lines[1] + "\r\n\r\n" +
		"Do this on that computer:\r\n" + steps[0] + "\r\n" + steps[1] + "\r\n\r\n" +
		which + "\r\n\r\n" +
		"You will get this email once for each account that stops downloading, not after every backup.\r\n\r\n" +
		"This message was sent by MailArchive's built-in backup alerts.\r\n"
	if body != wantStopped {
		t.Errorf("the stopped body (no computer name):\n%q\nwant\n%q", body, wantStopped)
	}
	for _, bad := range []string{"Computer:", "this computer are working", "that computer has stopped"} {
		if strings.Contains(body, bad) {
			t.Errorf("the stopped body says %q", bad)
		}
	}
	two := stopped
	two.Archive = "/Volumes/Backup/Mail/"
	two.TB = &TBBehind{Stopped: true, Accounts: 2, Lines: stopped.TB.Lines, Remedy: steps}
	if subject, body := c.subjectAndBody(two); subject != "MailArchive: Thunderbird stopped downloading (backups OK) — Mail" ||
		!strings.HasPrefix(body, "Your backups are working, but Thunderbird has stopped downloading 2 accounts' mail, so") {
		t.Errorf("two stopped accounts: %q\n%s", subject, body)
	}
	if subject, body := c.subjectAndBody(Event{Result: ResultTBBehind, Archive: "/m", TB: &TBBehind{Accounts: 3, Lines: []string{"a", "b", "c"}, Remedy: []string{"x"}}}); strings.Contains(subject, "after") || strings.Contains(subject, "3 days") ||
		!strings.HasPrefix(body, "Your backups are working. Thunderbird has not finished downloading 3 accounts' mail yet,") {
		t.Errorf("three still downloading: %q\n%s", subject, body)
	}

	// No header from a crafted line; no line may break the body's shape.
	evil := Event{Result: ResultTBBehind, Archive: "/a\r\nBcc: evil@example.org", Host: "h\r\nBcc: evil@example.org", TB: &TBBehind{Accounts: 1,
		Lines: []string{"x\r\nBcc: evil@example.org"}, Remedy: []string{"y\r\nBcc: evil@example.org", "z\nBcc: evil@example.org"}, Which: "w\r\nBcc: evil@example.org"}}
	headers, rest, _ := strings.Cut(string(c.Message(evil)), "\r\n\r\n")
	for _, line := range strings.Split(headers+"\r\n"+rest, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Errorf("a crafted line made its own line %q", line)
		}
	}
}
