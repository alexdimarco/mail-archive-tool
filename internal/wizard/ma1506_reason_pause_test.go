package wizard

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// reasonProfile is a Thunderbird profile in a new home (HOME and USERPROFILE point at it) holding
// an IMAP account folder per host, each with a message; prefs writes its prefs.js, one account per
// "host=folder" ("" folder for none), each named qz@host.
func reasonProfile(t *testing.T, hosts ...string) (profile string, prefs func(accts ...[2]string)) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	profile = filepath.Join(home, ".thunderbird", "abcd1234.default-release")
	for _, h := range hosts {
		d := filepath.Join(profile, "ImapMail", h)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		box := "From - Mon Mar  3 09:00:00 2025\nFrom: x@example.com\nSubject: s\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + h + "@ex>\n\nbody\n\n"
		if err := os.WriteFile(filepath.Join(d, "INBOX"), []byte(box), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "INBOX.msf"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(profile, "compatibility.ini"), []byte("[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return profile, func(accts ...[2]string) {
		t.Helper()
		var b strings.Builder
		var ids []string
		for i, a := range accts {
			id, key := "account"+string(rune('1'+i)), "server"+string(rune('1'+i))
			ids = append(ids, id)
			b.WriteString(`user_pref("mail.account.` + id + `.server", "` + key + `");` + "\n")
			b.WriteString(`user_pref("mail.server.` + key + `.hostname", "` + a[0] + `");` + "\n")
			b.WriteString(`user_pref("mail.server.` + key + `.type", "imap");` + "\n")
			b.WriteString(`user_pref("mail.server.` + key + `.name", "qz@` + a[0] + `");` + "\n")
			if a[1] != "" {
				b.WriteString(`user_pref("mail.server.` + key + `.directory-rel", "[ProfD]ImapMail/` + a[1] + `");` + "\n")
			}
		}
		b.WriteString(`user_pref("mail.accountmanager.accounts", "` + strings.Join(ids, ",") + `");` + "\n")
		if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// reasonArchiveJob makes an archive of the one folder in (auto-discovery off) by a real run, with a
// job file and an installed schedule's descriptor, and returns the archive, the job and the
// schedule's name.
func reasonArchiveJob(t *testing.T, in string) (out, jobPath, name string) {
	t.Helper()
	out = filepath.Join(t.TempDir(), "arch")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{in}, Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ := state.ReadLastRun(out); autoplace.RecordedName(lr, in) != "qz@"+filepath.Base(in) {
		t.Fatalf("the archive's record must name the account of %s: %+v", in, lr.Thunderbird)
	}
	jobPath = filepath.Join(t.TempDir(), "job.json")
	if err := job.Write(jobPath, job.Job{Name: jobFileName(jobPath), Out: out, Mode: "incremental", Inputs: []string{in}}); err != nil {
		t.Fatal(err)
	}
	name = schedule.DefaultNameFor(out)
	if err := schedule.WriteDescriptor(out, schedule.Descriptor{Name: name, Interval: "daily", At: "02:00", Job: []string{"mailarchive", "-out", out}}); err != nil {
		t.Fatal(err)
	}
	return out, jobPath, name
}

// lastPause is the reason of the last auto-pause email the rig sent, or "" for none.
func (g *localRunRig) lastPause() string {
	for i := len(g.sent) - 1; i >= 0; i-- {
		if g.sent[i].Result == alert.ResultPaused {
			return g.sent[i].Error
		}
	}
	return ""
}

// covers: MA-1506, MA-1504, MA-871, R18
// The words travel with the typed stop (docs/design-per-mailbox-archives.md §11, C13 and F-03): a
// one-folder archive whose account was removed for good is stopped by the scheduled run, whose
// failure email and Run now's note say C13's words; and at 30 days the scheduled run's pause says
// the reason's own words in its email and in the run log, in place of "reconnect the source, then …
// click Repair": for removed, and for moved (C2: the folder is still there, the account re-added in
// another). Positive twin first: an archive whose stop kept no reason (a plain mailbox file gone) is
// paused in today's words.
func TestThePauseAndTheNoticesSayTheReasonsWords(t *testing.T) {
	g := newLocalRunRig(t)
	// Positive twin: today's pause words.
	plainName := schedule.DefaultNameFor(g.out)
	rec := recordRemovals(t, plainName)
	if err := schedule.WriteDescriptor(g.out, schedule.Descriptor{Name: plainName, Interval: "daily", At: "02:00", Job: []string{"mailarchive", "-out", g.out}}); err != nil {
		t.Fatal(err)
	}
	seedDueStreak(t, g.out)
	RunHeadless(g.jobAt(filepath.Join(g.dir, "fail.json"), filepath.Join(g.dir, "gone-Inbox")))
	if len(*rec) != 1 || g.lastPause() != AutoPauseWhy {
		t.Fatalf("positive twin: a stop with no reason pauses in today's words: removed %v, reason %q", *rec, g.lastPause())
	}

	profile, prefs := reasonProfile(t, "imap.qza.example", "imap.qzb.example")
	a, b := filepath.Join(profile, "ImapMail", "imap.qza.example"), filepath.Join(profile, "ImapMail", "imap.qzb.example")
	prefs([2]string{"imap.qza.example", "imap.qza.example"}, [2]string{"imap.qzb.example", "imap.qzb.example"})
	outA, jobA, nameA := reasonArchiveJob(t, a)
	outB, jobB, nameB := reasonArchiveJob(t, b)
	rec = recordRemovals(t, nameA, nameB)

	// Removed for good: the account gone from Thunderbird with its folder.
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	prefs([2]string{"imap.qza.example", "imap.qza.example"})
	g.sent = nil
	if code := RunHeadless(jobB); code != 1 || len(g.sent) != 1 || g.sent[0].Result != alert.ResultFailed || g.sent[0].Error != autoplace.RemovedText(b) {
		t.Fatalf("the failure email says C13's words: exit %d, sent %+v", code, g.sent)
	}
	if _, err := RunJobInProcess(context.Background(), context.Background(), jobB, log.New(io.Discard, "", 0), RunNow{}); err == nil || err.Error() != autoplace.RemovedText(b) {
		t.Errorf("Run now's note says C13's words: %v", err)
	}
	seedDueStreak(t, outB)
	s, _ := state.ReadSourceStreak(outB)
	RunHeadless(jobB)
	// Amended 2026-11-25 (the friction review's F12; nothing weakened): an email's "here" pointed
	// nowhere; both pauses' words said "Its mail stays here." before.
	wantB := "MailArchive paused this archive's scheduled backup: Thunderbird has not had its account since " + s.FirstUnreachable.Format("Jan 2, 2006") + ". Its mail stays in the archive."
	if len(*rec) != 1 || g.lastPause() != wantB {
		t.Errorf("the removed account's pause: removed %v, reason %q, want %q", *rec, g.lastPause(), wantB)
	}
	if logged, _ := os.ReadFile(filepath.Join(outB, "mailarchive.log")); !strings.Contains(string(logged), "auto-paused: "+wantB) || strings.Contains(string(logged), "reconnect the source") {
		t.Errorf("the run log says the reason's pause words, not reconnect the source:\n%s", logged)
	}

	// Moved: the account re-added, kept in imap.qza.example-1; the old folder still there.
	if err := os.MkdirAll(filepath.Join(profile, "ImapMail", "imap.qza.example-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	prefs([2]string{"imap.qza.example", "imap.qza.example-1"})
	seedDueStreak(t, outA)
	s, _ = state.ReadSourceStreak(outA)
	RunHeadless(jobA)
	wantA := "MailArchive paused this archive's scheduled backup: Thunderbird has not used its mail folder since " + s.FirstUnreachable.Format("Jan 2, 2006") + ". Its mail stays in the archive. To back that account up, click Make a new archive… and choose it again."
	if len(*rec) != 2 || g.lastPause() != wantA || wantA != health.SourceReasonPauseWordsAt(outA) {
		t.Errorf("the moved account's pause: removed %v, reason %q, want %q", *rec, g.lastPause(), wantA)
	}
	if lr, _, _ := state.ReadLastRun(outA); lr.SourceReason != state.SourceReasonMoved || !strings.HasPrefix(lr.Error, "Thunderbird no longer uses "+a+":") {
		t.Errorf("the moved stop's record: %q %q", lr.SourceReason, lr.Error)
	}
}
