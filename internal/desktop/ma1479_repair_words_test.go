package desktop

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// The words for an archive 1.7.4 stopped because a mail account's folder is gone
// (docs/design-1.7.5-fixes.md §1.3, "Archives 1.7.4 already stopped"; MA-1479): status, the
// Your-archives banner and the Settings card say Repair or Run now only when
// health.GoneMailboxRepairable says a run would now get past the gone mailbox.

// Amended 2026-11-20 by the 1.7.5 friction review (F10), each restated word for word at the same
// strength: the Repair forms name the missing mailbox ("(the missing mailbox: PATH)", PATH the input
// the record refused on, filled in by fmt.Sprintf), say "MailArchive now skips" for "this version
// skips", and the Settings card says where Repair is ("click Repair on the Scheduled backup card").
// The words were: "…since Oct 12, 2026 — if a mail account was removed, … : this version skips a
// mailbox that is gone and backs up the others; …", "…since Oct 12, 2026. If a mail account was
// removed, … : this version skips …", and "If a mail account was removed, click Repair: this version
// skips a mailbox that is gone and backs up the others." Today's words are unchanged.
const (
	todaysStatus  = "this archive's source has not been reachable since Oct 12, 2026 — reconnect the source (its mailbox or mail folder) to resume backups"
	repairStatus  = "this archive's source has not been reachable since Oct 12, 2026 (the missing mailbox: %s) — if a mail account was removed, click Repair (or Run now) on the archive's Scheduled backup card in MailArchive Desktop: MailArchive now skips a mailbox that is gone and backs up the others; otherwise reconnect the source (its mailbox or mail folder) to resume backups"
	runNowStatus  = "this archive's source has not been reachable since Oct 12, 2026 (the missing mailbox: %s) — if a mail account was removed, click Run now on the archive's Scheduled backup card in MailArchive Desktop: MailArchive now skips a mailbox that is gone and backs up the others; otherwise reconnect the source (its mailbox or mail folder) to resume backups"
	repairBanner  = "This archive's source hasn't been reachable since Oct 12, 2026 (the missing mailbox: %s). If a mail account was removed, click Repair (or Run now) on its Scheduled backup card: MailArchive now skips a mailbox that is gone and backs up the others. Repair starts the schedule again; Run now backs up once. Otherwise reconnect the source (its mailbox or mail folder) to resume backups."
	runNowBanner  = "This archive's source hasn't been reachable since Oct 12, 2026 (the missing mailbox: %s). If a mail account was removed, click Run now on its Scheduled backup card: MailArchive now skips a mailbox that is gone and backs up the others; its backup runs only when you click Run now. Otherwise reconnect the source (its mailbox or mail folder)."
	switchRepair  = "This archive's source hasn't been reachable since Oct 12, 2026 (the missing mailbox: %s). If a mail account was removed, click Switch to this archive, then click Repair (or Run now) on its Scheduled backup card: MailArchive now skips a mailbox that is gone and backs up the others. Repair starts the schedule again; Run now backs up once. Otherwise reconnect the source (its mailbox or mail folder) to resume backups."
	switchRunNow  = "This archive's source hasn't been reachable since Oct 12, 2026 (the missing mailbox: %s). If a mail account was removed, click Switch to this archive, then click Run now on its Scheduled backup card: MailArchive now skips a mailbox that is gone and backs up the others; its backup runs only when you click Run now. Otherwise reconnect the source (its mailbox or mail folder)."
	todaysBanner  = "This archive's source hasn't been reachable since Oct 12, 2026 — reconnect the source (its mailbox or mail folder) to resume backups."
	todaysRunNow  = "This archive's source hasn't been reachable since Oct 12, 2026 — reconnect the source (its mailbox or mail folder), then click Run now on its Scheduled backup card: its backup runs only when you click Run now."
	settingsAdded = "If a mail account was removed, click Repair on the Scheduled backup card: MailArchive now skips a mailbox that is gone and backs up the others."
)

// goneHome is a home whose Thunderbird profile, where auto-discovery looks, keeps account folders
// a and b (each a folder only), HOME and USERPROFILE pointing at it. It returns a, b and the
// profile.
func goneHome(t *testing.T) (a, b, profile string) {
	t.Helper()
	home := isolateHome(t)
	t.Setenv("USERPROFILE", home)
	profile = filepath.Join(home, ".thunderbird", "abcd1234.default-release")
	a, b = filepath.Join(profile, "ImapMail", "imap.qza.example"), filepath.Join(profile, "ImapMail", "imap.qzb.example")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return a, b, profile
}

// stoppedArchive is an archive 1.7.4 stopped: its last run failed with err, its job jobArgs, its
// source streak past the surface threshold with kind, and its schedule record interval.
func stoppedArchive(t *testing.T, errText string, jobArgs []string, kind, interval string, encrypted bool) string {
	t.Helper()
	out := t.TempDir()
	writeManifest(t, out)
	t0 := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: t0, LastUnreachable: t0.Add(state.SurfaceAfter), LastKind: kind}); err != nil {
		t.Fatal(err)
	}
	at := t0.Add(state.SurfaceAfter)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunFailed, Started: at, Finished: at, Error: errText,
		Job: append([]string{"-out", out, "-mode", "incremental"}, jobArgs...)}); err != nil {
		t.Fatal(err)
	}
	if encrypted {
		// A record marked encrypted that still carries the job and the error (planted: an
		// encrypted archive's writer never writes them), so only the helper's own plaintext
		// check can say no.
		p := filepath.Join(out, state.LastRunName)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		raw = []byte(strings.Replace(string(raw), `"version": 1,`, `"version": 1, "encrypted": true,`, 1))
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if lr, _, _ := state.ReadLastRun(out); !lr.Encrypted || len(lr.Job) == 0 || lr.Error != errText {
			t.Fatalf("the planted encrypted record: %+v", lr)
		}
	}
	name := schedule.DefaultNameFor(out)
	jp, err := job.PathFor(name)
	if err != nil {
		t.Fatal(err)
	}
	spec := schedule.Spec{Name: name, Interval: schedule.Daily, At: "03:00", Exe: "/opt/mailarchive", Args: []string{"-job", jp}, Out: out}
	d := schedule.DescribeSpec(spec)
	if interval == "manual" {
		d = schedule.DescribeManual(spec)
	}
	if err := schedule.WriteDescriptor(out, d); err != nil {
		t.Fatal(err)
	}
	return out
}

// bannerOf is the Your-archives page drawn with out listed, current or beside another archive.
func bannerOf(t *testing.T, out string, current bool) (string, ArchiveRow) {
	t.Helper()
	other := t.TempDir()
	writeManifest(t, other)
	idxPath := filepath.Join(t.TempDir(), archindex.Name)
	if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: out}, {Path: other}}}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(t.TempDir(), "s.json")
	cur := other
	if current {
		cur = out
	}
	if err := SaveSettings(settings, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{SettingsPath: settings, IndexPath: idxPath, GOOS: "darwin"}
	rows := cfg.archiveRows(archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: out}}})
	if len(rows) != 1 {
		t.Fatalf("rows: %+v", rows)
	}
	page, _ := getPage(t, DashboardHandler(cfg))
	return page, rows[0]
}

// statusOf is status's report of out, as one text.
func statusOf(out string) string {
	return strings.Join(health.Assess(health.Gather(out, ""), time.Now()).Reasons, "\n")
}

// covers: MA-1479, MA-692, MA-693, MA-949, R18
// The words for a source-lost archive come from one helper, health.GoneMailboxRepairable, for
// status and the banner alike (X10). Positive twin first: a plaintext record with -auto, failed
// with the refusal of a listed input autoplace.GoneSkippable accepts now, another listed input
// there, and streak kind local-source-unreachable, says Repair or Run now: in status (by the
// archive's How often), and on the banner in all four of MA-949's forms (scheduled or Run now only,
// current or not). Each other case keeps today's words exactly, in both: the refused input now
// outside the places, a link, or with its group folder gone; no listed input there; the record's
// error naming no listed input; a record without -auto; another streak kind; an encrypted
// archive.
func TestTheRepairWordsShowOnlyWhereARunWouldGetPastTheGoneMailbox(t *testing.T) {
	a, b, _ := goneHome(t)
	os.RemoveAll(b)
	auto := []string{"-auto", "-input", a, "-input", b}
	for _, c := range []struct {
		interval string
		current  bool
		status   string
		banner   string
	}{
		{"daily", true, repairStatus, repairBanner},
		{"daily", false, repairStatus, switchRepair},
		{"manual", true, runNowStatus, runNowBanner},
		{"manual", false, runNowStatus, switchRunNow},
	} {
		out := stoppedArchive(t, autoplace.GoneText(b), auto, "local-source-unreachable", c.interval, false)
		if !health.GoneMailboxRepairable(out) {
			t.Fatalf("positive twin %s, current %v: the helper says no", c.interval, c.current)
		}
		if want := fmt.Sprintf(c.status, b); !strings.Contains(statusOf(out), "WARN: "+want) {
			t.Errorf("positive twin %s: status:\n%s\nwant %q", c.interval, statusOf(out), want)
		}
		page, row := bannerOf(t, out, c.current)
		if want := fmt.Sprintf(c.banner, b); !row.SourceLostRepair || !strings.Contains(page, want) {
			t.Errorf("positive twin %s, current %v: the banner (row %+v) lacks %q", c.interval, c.current, row, want)
		}
	}

	usb := filepath.Join(t.TempDir(), "USB", "Inbox")
	link := filepath.Join(filepath.Dir(a), "imap.qzlink.example")
	canLink := os.Symlink(filepath.Join(t.TempDir(), "gone-target"), link) == nil // not on every Windows account
	goneProfile := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(a))), "gone.profile", "ImapMail", "imap.qzc.example")
	for name, c := range map[string]struct {
		errText string
		job     []string
		kind    string
		enc     bool
	}{
		"the refused input outside the places":  {autoplace.GoneText(usb), []string{"-auto", "-input", a, "-input", usb}, "local-source-unreachable", false},
		"the refused input a link":              {autoplace.GoneText(link), []string{"-auto", "-input", a, "-input", link}, "local-source-unreachable", false},
		"the refused input's group folder gone": {autoplace.GoneText(goneProfile), []string{"-auto", "-input", a, "-input", goneProfile}, "local-source-unreachable", false},
		"no listed input there":                 {autoplace.GoneText(b), []string{"-auto", "-input", b}, "local-source-unreachable", false},
		"the error naming no listed input":      {autoplace.GoneText(filepath.Join(filepath.Dir(a), "imap.other.example")), auto, "local-source-unreachable", false},
		"a record without -auto":                {autoplace.GoneText(b), []string{"-input", a, "-input", b}, "local-source-unreachable", false},
		"another streak kind":                   {autoplace.GoneText(b), auto, "m365-unreachable", false},
		"an encrypted archive":                  {autoplace.GoneText(b), auto, "local-source-unreachable", true},
	} {
		if name == "the refused input a link" && !canLink {
			t.Logf("%s: not run, this system cannot make a link here", name)
			continue
		}
		for _, interval := range []string{"daily", "manual"} {
			out := stoppedArchive(t, c.errText, c.job, c.kind, interval, c.enc)
			if health.GoneMailboxRepairable(out) {
				t.Errorf("%s (%s): the helper says yes", name, interval)
			}
			got := statusOf(out)
			if !strings.Contains(got, "WARN: "+todaysStatus) || strings.Contains(got, "if a mail account was removed") {
				t.Errorf("%s (%s): status must keep today's words:\n%s", name, interval, got)
			}
			want := todaysBanner
			if interval == "manual" {
				want = todaysRunNow
			}
			page, row := bannerOf(t, out, true)
			if row.SourceLostRepair || !strings.Contains(page, want) || strings.Contains(page, "If a mail account was removed") {
				t.Errorf("%s (%s): the banner must keep today's words (row %+v)", name, interval, row)
			}
		}
	}
}

// covers: MA-1479, MA-860, R37
// The Settings card's auto-paused line, drawn for the current archive whose entry auto-pause took
// out, adds "If a mail account was removed, click Repair: …" only when the helper says yes.
// Positive twin first: a real "All of them" archive whose schedule Settings made, then 1.7.4's
// refusals of its gone account (its record failed on that input, its streak past 30 days, its
// entry taken out) says it; with the profile's ImapMail folder gone too, it keeps today's line
// alone.
func TestTheSettingsCardSaysRepairOnlyWhereItWorks(t *testing.T) {
	a, b, _ := goneHome(t)
	for _, d := range []string{a, b} {
		box := "From - Mon Mar  3 09:00:00 2025\nFrom: x@example.com\nSubject: s\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + filepath.Base(d) + "@ex>\n\nbody\n\n"
		if err := os.WriteFile(filepath.Join(d, "INBOX"), []byte(box), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "INBOX.msf"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "arch")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{a, b}, Auto: true, Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	lr, _, _ := state.ReadLastRun(out)
	os.RemoveAll(b)
	at := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC).Add(31 * 24 * time.Hour)
	lr.Status, lr.Started, lr.Finished, lr.Error = state.RunFailed, at, at, autoplace.GoneText(b)
	if err := state.WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 31, FirstUnreachable: time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC), LastUnreachable: at, LastKind: "local-source-unreachable"}); err != nil {
		t.Fatal(err)
	}
	delete(g.fake.tasks, out) // auto-pause took the entry out, the descriptor kept
	stopped := "Backups stopped: this archive&#39;s source has been unreachable since Oct 12, 2026."
	if s := cardOf(g.page(), "sec-settings"); !strings.Contains(s, stopped+" "+settingsAdded) {
		t.Fatalf("positive twin: the auto-paused Settings line must add the Repair sentence:\n%s", s)
	}
	if err := os.RemoveAll(filepath.Dir(a)); err != nil { // the profile's ImapMail folder: Repair would fail again
		t.Fatal(err)
	}
	if s := cardOf(g.page(), "sec-settings"); !strings.Contains(s, stopped) || strings.Contains(s, settingsAdded) {
		t.Errorf("with the group folder gone the line keeps today's words alone:\n%s", s)
	}
}
