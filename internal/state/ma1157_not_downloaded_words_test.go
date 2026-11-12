package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1157, R18, S29
// The not-downloaded sentence is worded by the kind of mail the run read (the 1.6.1 friction
// review's P7: a plain mbox message with no body was said as "your mail app (such as
// Thunderbird) has not downloaded it"): Thunderbird's own store names Thunderbird; another mail
// app's store names "your mail app"; a mail file no mail app keeps says the file holds only part
// of the messages and to copy it again from its mail program, and names no mail app; Microsoft
// 365, which hands over each message whole, says they came into the archive another way; a kind
// not known (an older record, a mix) blames nobody. Positive twin first: Thunderbird is named
// for Thunderbird's store.
func TestTheNotDownloadedWordsSayWhereTheMailCameFrom(t *testing.T) {
	tb := NotDownloadedSentence(3, FromThunderbird)
	if tb != "3 messages are not on this computer in full: Thunderbird has not downloaded them yet. Later backups add them once it has; opening them in Thunderbird makes it download them sooner." {
		t.Fatalf("the positive twin: %q", tb)
	}
	app := NotDownloadedSentence(1, FromMailApp)
	file := NotDownloadedSentence(2, FromMailFile)
	m365 := NotDownloadedSentence(2, FromMicrosoft365)
	unknown := NotDownloadedSentence(2, "")
	planted := NotDownloadedSentence(2, "<b>thunderbird</b>")
	for _, c := range []struct{ name, got, want string }{
		{"another mail app", app, "1 message is not on this computer in full: your mail app has not downloaded it yet. Later backups add it once it has; opening it in your mail app makes it download it sooner."},
		{"a mail file", file, "2 messages are not in the archive in full: the mail file holds only part of these messages; if it is a copy, copy it again from its mail program. Later backups add them once the file holds them in full."},
		{"Microsoft 365", m365, "2 messages in this archive are not in it in full. They were not read from Microsoft 365, which hands over each message whole, but from mail added to this archive another way; later backups of that mail add them once it holds them in full."},
		{"not known", unknown, "2 messages are not in the archive in full yet: the mail they were read from does not hold all of their content yet (a mail app still downloading, or a mail file that is only a partial copy). Later backups add them once it does."},
		{"a planted kind", planted, unknown},
	} {
		if c.got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, c.got, c.want)
		}
	}
	if s := NotDownloadedSentence(1, FromMailFile); !strings.Contains(s, "only part of this message") {
		t.Errorf("one message from a file: %q", s)
	}
	for _, s := range []string{file, m365, unknown} {
		assure.Reached(t, !strings.Contains(s, "Thunderbird") && !strings.Contains(s, "has not downloaded"), "no mail app is blamed: "+s)
	}
}

// covers: MA-1157, R18, S29
// Where the run read its mail is recorded beside the not-downloaded count, additively: the
// last-run record and its history entry carry "fillable_from" from a closed set (a planted
// value reads as not known); an encrypted archive's record and history never carry it.
// Positive twin first: a plaintext record keeps the kind.
func TestWhereTheMailCameFromIsRecorded(t *testing.T) {
	out := t.TempDir()
	at := time.Date(2026, 11, 7, 17, 0, 0, 0, time.UTC)
	if err := WriteLastRun(out, LastRun{Status: RunOK, Started: at, Finished: at.Add(time.Minute), Fillable: 2, FillableFrom: FromMailFile}); err != nil {
		t.Fatal(err)
	}
	lr, _, _ := ReadLastRun(out)
	if lr.FillableFrom != FromMailFile {
		t.Fatalf("the positive twin: the record's kind %q", lr.FillableFrom)
	}
	if e := RunHistoryEntryFrom(lr); e.FillableFrom != FromMailFile || e.Fillable != 2 {
		t.Errorf("the history entry: %+v", e)
	}
	if err := AppendRunHistory(out, RunHistoryEntryFrom(lr)); err != nil {
		t.Fatal(err)
	}
	if h, _ := ReadRunHistory(out); len(h) != 1 || h[0].FillableFrom != FromMailFile {
		t.Errorf("the history read back: %+v", h)
	}

	planted := `[{"when":"2026-11-07T17:00:00Z","status":"ok","fillable":3,"fillable_from":"<script>"}]`
	if err := os.WriteFile(filepath.Join(out, RunHistoryName), []byte(planted), 0o600); err != nil {
		t.Fatal(err)
	}
	if h, _ := ReadRunHistory(out); len(h) != 1 || h[0].FillableFrom != "" {
		t.Errorf("a planted kind must read as not known: %+v", h)
	}

	enc := t.TempDir()
	if err := WriteLastRun(enc, LastRun{Status: RunOK, Finished: at, Encrypted: true, Fillable: 2, FillableFrom: FromThunderbird}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(enc, LastRunName))
	e := RunHistoryEntryFrom(LastRun{Status: RunOK, Finished: at, Encrypted: true, FillableFrom: FromThunderbird})
	assure.Reached(t, !strings.Contains(string(raw), "fillable_from") && e.FillableFrom == "", "an encrypted archive keeps no kind: "+string(raw))
}
