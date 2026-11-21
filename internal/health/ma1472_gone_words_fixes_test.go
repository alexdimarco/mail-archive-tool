package health

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1472, R18
// The day the WARN names is honoured (the 1.7.5 adversarial pass's CON-01): "from the first backup on
// or after DAY this is only a note" names the first local day every backup of which is 14 days or
// more after the first-found time, so a backup on that day is never still a WARN. The skeptic's case:
// first found Nov 1 at 15:00 local; the Nov 15 02:00 backup WARNs and names Nov 16; a backup on Nov 16
// at 00:30 is a note. Positive twin first: a first-found time at local midnight names its own day 14
// days on.
func TestTheNoteDayTheWarnNamesIsHonoured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".thunderbird", "abcd1234.default-release", "ImapMail", "imap.qzb.example")
	w := state.RunWarning{Code: state.MailboxGoneCode, Message: "x", Path: path}

	w.Since = time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local)
	at := time.Date(2026, 11, 10, 2, 0, 0, 0, time.Local)
	if got, _ := goneReport(t, state.RunOK, at, w, at); !strings.Contains(got, "on or after Nov 15, 2026 this is only a note") {
		t.Fatalf("positive twin: a first-found time at local midnight names its own day 14 days on:\n%s", got)
	}

	w.Since = time.Date(2026, 11, 1, 15, 0, 0, 0, time.Local)
	at = time.Date(2026, 11, 15, 2, 0, 0, 0, time.Local)
	got, rep := goneReport(t, state.RunOK, at, w, at)
	if !rep.has(state.MailboxGoneCode) || !strings.Contains(got, "on or after Nov 16, 2026 this is only a note") {
		t.Errorf("the Nov 15 02:00 backup WARNs and names Nov 16:\n%s", got)
	}
	at = time.Date(2026, 11, 16, 0, 30, 0, 0, time.Local)
	if _, rep := goneReport(t, state.RunOK, at, w, at); rep.has(state.MailboxGoneCode) || !rep.has("mailbox_gone_note") {
		t.Errorf("a backup on the named day, Nov 16 at 00:30, is a note")
	}
}

// covers: MA-1472, R18
// A mailbox_gone warning with no path (a 1.7.4 program's write-back drops it, or a damaged record) is
// never said as a broken sentence (the 1.7.5 adversarial pass's CON-03): status WARNs in a fixed
// sentence that names no folder. Positive twin first: with a path, the WARN names it.
func TestAGoneWarningWithNoPathIsNeverABrokenSentence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	started := time.Date(2026, 11, 20, 2, 0, 0, 0, time.UTC)
	path := filepath.Join(home, ".thunderbird", "p", "ImapMail", "imap.qzb.example")
	if got, _ := goneReport(t, state.RunOK, started, state.RunWarning{Code: state.MailboxGoneCode, Message: "x", Path: path, Since: started}, started); !strings.Contains(got, "no longer on this computer: "+path+", not found since") {
		t.Fatalf("positive twin: with a path, the WARN names it:\n%s", got)
	}
	got, rep := goneReport(t, state.RunOK, started, state.RunWarning{Code: state.MailboxGoneCode, Message: "Not found, so skipped: …"}, started)
	if strings.Contains(got, "computer: , not found") || strings.Contains(got, "put it back where it was") || !rep.has(state.MailboxGoneCode) ||
		!strings.Contains(got, "its record does not say which") {
		t.Errorf("a warning with no path is said in a fixed sentence, still a WARN:\n%s", got)
	}
}

// covers: MA-1472, R18
// The WARN's remedy fits what is known (the 1.7.5 friction review's F4, F9, F11 and F12): an account
// Thunderbird still lists (the run recorded it as listed) is told to let Thunderbird download its
// mail, never "add the account back"; an Outlook .pst moved away is told to put it back in its folder
// or choose it in its new place; the note names the button that ends it (Make a new archive…); and
// with only Local Folders left in the archive's job, "Local Folders is still backed up". Positive
// twin first: an account Thunderbird no longer lists keeps "add the account back in Thunderbird", and
// two remaining mailboxes keep "the other mailboxes".
func TestTheGoneWarnsRemedyFitsWhatIsKnown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	profile := filepath.Join(home, ".thunderbird", "abcd1234.default-release")
	b, a, local := filepath.Join(profile, "ImapMail", "imap.qzb.example"), filepath.Join(profile, "ImapMail", "imap.qza.example"), filepath.Join(profile, "Mail", "Local Folders")
	started := time.Date(2026, 11, 20, 2, 0, 0, 0, time.UTC)
	report := func(job []string, w state.RunWarning, at time.Time) string {
		in := Input{Out: t.TempDir(), LastRunState: state.LastRunPresent, LastRun: state.LastRun{Status: state.RunOK, Started: at, Finished: at, Job: job, Warnings: []state.RunWarning{w}}}
		return strings.Join(Assess(in, at).Reasons, "\n")
	}
	job := func(inputs ...string) []string {
		j := []string{"-out", "/x", "-mode", "incremental", "-auto"}
		for _, in := range inputs {
			j = append(j, "-input", in)
		}
		return j
	}
	gone := state.RunWarning{Code: state.MailboxGoneCode, Message: "x", Path: b, Name: "qzsam@qz.example", Since: started}
	got := report(job(a, b, local), gone, started)
	if !strings.Contains(got, "If you didn't remove it, add the account back in Thunderbird and the next backup finds it again.") || !strings.Contains(got, "and the other mailboxes are still backed up") {
		t.Fatalf("positive twin: an account no longer listed, two mailboxes left:\n%s", got)
	}
	listed := gone
	listed.Listed = true
	if got := report(job(a, b, local), listed, started); strings.Contains(got, "add the account back") ||
		!strings.Contains(got, "Thunderbird still lists this account: open Thunderbird and let it download the account's mail; the next backup finds it again.") {
		t.Errorf("F4: an account Thunderbird still lists:\n%s", got)
	}
	if got := report(job(b, local), gone, started); !strings.Contains(got, "and Local Folders is still backed up") || strings.Contains(got, "other mailboxes") {
		t.Errorf("F12: only Local Folders left:\n%s", got)
	}
	pst := filepath.Join(home, "Documents", "Outlook Files", "archive 2019.pst")
	moved := state.RunWarning{Code: state.MailboxGoneCode, Message: "x", Path: pst, Since: started}
	if got := report(job(a, pst), moved, started); strings.Contains(got, "add the account back") ||
		// Amended 2026-11-20 by the 1.7.5 re-verify (RF-05), word for word at the same strength.
		!strings.Contains(got, "If you moved this file, put it back in "+filepath.Dir(pst)+" and the next backup finds it again. To back it up from its new place instead, click Make a new archive… in MailArchive Desktop and choose the file there: that makes a separate archive.") {
		t.Errorf("F9: a .pst moved away:\n%s", got)
	}
	later := started.Add(20 * 24 * time.Hour)
	if got := report(job(a, b, local), gone, later); strings.Contains(got, "set the archive up again in the wizard") ||
		!strings.Contains(got, "to end it, click Make a new archive… in MailArchive Desktop, choose All of them and this same archive folder, and answer Yes to keeping it current.") {
		t.Errorf("F11: the note names the button:\n%s", got)
	}
}
