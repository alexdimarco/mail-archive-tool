package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// stuckWords plants a holder line that started 30 hours before now with the given host field and
// returns the words a backup refused by it says (busyWords: the one producer every surface takes
// them from — Desktop, the last-run record and so `status`, the alert email, the notice).
func stuckWords(t *testing.T, self, hostField string, now time.Time) string {
	t.Helper()
	prev := thisHost
	thisHost = func() string { return self }
	t.Cleanup(func() { thisHost = prev })
	lock := filepath.Join(t.TempDir(), lockfile.Name)
	line := "pid=1 started=" + now.Add(-30*time.Hour).UTC().Format(time.RFC3339) + " " + hostField + " nonce=n verb=export\n"
	if err := os.WriteFile(lock, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return busyWords(lock, now)
}

// calledName is the name the words give another computer, "" when they name none.
func calledName(w string) string {
	const pre = "which calls itself "
	i := strings.Index(w, pre)
	if i < 0 {
		return ""
	}
	rest := w[i+len(pre):]
	return rest[:strings.Index(rest, ", and still")]
}

var stuckNow = time.Date(2026, 11, 7, 17, 30, 0, 0, time.Local)

// covers: MA-1164, R18, R5, S25
// This computer's own stuck backup is never said to be on another computer when its name has a
// character a computer name's words do not keep (the polish-161 adversarial pass, H2: a Windows
// name such as JOSÉ-PC was cleaned on the lock's side only, so it never matched itself and the
// owner was told "restarting this one does not"): lockfile.SameHost cleans both names first.
// Positive twin first: another computer is still named.
func TestThisComputersOwnAccentedNameIsThisComputer(t *testing.T) {
	if other := stuckWords(t, "JOSÉ-PC", "host=OFFICE-PC", stuckNow); calledName(other) != "OFFICE-PC" {
		t.Fatalf("the positive twin: another computer: %q", other)
	}
	if !lockfile.SameHost("JOSÉ-PC", "JOS-PC") || !lockfile.SameHost("josé-pc.home", "JOSÉ-PC") || lockfile.SameHost("JOSÉ-PC", "OFFICE-PC") {
		t.Error("SameHost does not compare the two names as the holder line keeps them")
	}
	own := stuckWords(t, "JOSÉ-PC", "host=JOSÉ-PC", stuckNow)
	assure.Reached(t, !strings.Contains(own, "another computer") && strings.Contains(own, "restart the computer to end it"),
		"this computer's own stuck backup keeps this computer's words: "+own)
}

// covers: MA-1165, R18, S25
// Another computer is named by the part of its name before the first dot, what a person calls the
// computer (the adversarial pass, H3: a planted dotted name such as your-bank.com went into the
// alert email's body, where a mail app may underline it as a web address); the email's subject is
// still its fixed words and nothing planted reaches a header (E1, refuted, kept as a guard).
// Positive twin first: a name with no dot is said whole.
func TestAnotherComputerIsNamedWithoutItsDomain(t *testing.T) {
	if w := stuckWords(t, "ALEX-MAC", "host=OFFICE-PC", stuckNow); calledName(w) != "OFFICE-PC" {
		t.Fatalf("the positive twin: %q", w)
	}
	if w := stuckWords(t, "ALEX-MAC", "host=Alexs-MacBook-Pro.local", stuckNow); calledName(w) != "Alexs-MacBook-Pro" {
		t.Errorf("a full DNS name: %q", w)
	}
	w := stuckWords(t, "ALEX-MAC", "host=your-bank.com\r\nBcc: x@evil.example", stuckNow)
	c := alert.Config{Host: "smtp.example.com", Port: 587, From: "a@example.com", To: "b@example.com"}
	msg := string(c.Message(alert.Event{Result: alert.ResultFailed, Busy: true, Archive: "/vol/A", Error: w, Host: "ALEX-MAC", When: stuckNow}))
	head, body, _ := strings.Cut(msg, "\r\n\r\n")
	if strings.Contains(head, "your-bank") || strings.Contains(strings.ToLower(head), "bcc") || !strings.Contains(head, "Subject: MailArchive backup skipped, the archive is still in use: /vol/A\r\n") {
		t.Errorf("planted text in the headers, or the subject not its fixed words:\n%s", head)
	}
	assure.Reached(t, calledName(w) == "your-bank" && strings.Contains(body, "which calls itself your-bank,") && !strings.Contains(body, "your-bank.com"),
		"the email names the computer without a dotted name: "+calledName(w))
}

// covers: MA-1166, R18, R5, S25
// The words do not state a planted computer name as fact (the adversarial pass, H4: anyone who can
// write the archive folder and hold its lock could name a real, important machine to be
// restarted): another computer "which calls itself NAME" is still using the archive; restart it
// only if it is yours; if it is not recognised, check who else uses the folder before restarting
// anything. The name is said once. Positive twin first: a stuck backup on this computer keeps its
// words, which name none.
func TestAStuckHolderElsewhereIsNamedAsItCallsItself(t *testing.T) {
	here := stuckWords(t, "ALEX-MAC", "host=alex-mac.home", stuckNow)
	if !strings.Contains(here, "restart the computer to end it; the next backup then runs as usual.") || strings.Contains(here, "calls itself") {
		t.Fatalf("the positive twin: %q", here)
	}
	w := stuckWords(t, "ALEX-MAC", "host=FILESERVER", stuckNow)
	want := "Another backup of this archive is already running (started " + stuckNow.Add(-30*time.Hour).Format("15:04 on 2006-01-02") +
		") — this backup was skipped. It started more than a day ago on another computer, which calls itself FILESERVER, and still keeps this archive in use (its lock file, " +
		lockfile.Name + "), so it may be stuck. If that computer is yours and the backup should have finished by now, restart it to end the backup (restarting this one does not); " +
		"if you don't recognise it, check who else uses this folder before restarting anything. The next backup then runs as usual."
	assure.Reached(t, w == want && strings.Count(w, "FILESERVER") == 1 && !strings.Contains(w, "restart the computer FILESERVER"),
		"a planted name is said as what the computer calls itself, once:\n got "+w+"\nwant "+want)
}

// covers: MA-1153, R18, S25
// Every planted spelling of the holder's computer name reaches the words only as a computer name's
// characters, at most 64, with no control, bidi, zero-width, markup or shell character (the
// adversarial pass, H1, refuted: kept as a guard). Positive twin first: a plain name is said.
func TestAPlantedHolderNameKeepsOnlyAComputerNamesCharacters(t *testing.T) {
	if w := stuckWords(t, "ALEX-MAC", "host=OFFICE-PC", stuckNow); calledName(w) != "OFFICE-PC" {
		t.Fatalf("the positive twin: %q", w)
	}
	cases := map[string]string{
		"bidi RLO": "host=‮CP-ECIFFO", "bidi isolate": "host=OFFICE⁦-PC⁩", "zero-width": "host=OFF​ICE-PC",
		"fullwidth": "host=ＯＦＦＩＣＥ", "cyrillic o": "host=оFFICE-PC", "newline": "host=OFFICE\r\nSubject: x\r\n",
		"nul": "host=OFF\x00ICE", "esc": "host=\x1b]0;pwn\x07OFFICE", "html": `host="><a href=javascript:alert(1)>x</a>`,
		"shell": "host=$(reboot);rm${IFS}-rf`id`|&&", "leading dash": "host=--help", "long": "host=" + strings.Repeat("A", 5000),
		"this computer": "host=THIS-COMPUTER", "two host fields": "host=FIRST host=SECOND", "only dots": "host=....",
	}
	named := 0
	for name, field := range cases {
		w := stuckWords(t, "ALEX-MAC", field, stuckNow)
		if strings.ContainsAny(w, "\r\n\x00\x1b<>\"`$|&‮⁦​") {
			t.Errorf("%s: an unsafe character in the words: %q", name, w)
		}
		h := calledName(w)
		for _, r := range h {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				t.Errorf("%s: the name %q keeps %U", name, h, r)
			}
		}
		if len(h) > 64 {
			t.Errorf("%s: a name %d long", name, len(h))
		}
		if h != "" {
			named++
		}
	}
	assure.Reached(t, named > 0, "planted names were said, and only as a computer name's characters")
}
