package alert

import (
	"mime"
	"strings"
	"testing"
)

// sentSubject is the message's Subject header line, without "Subject: ", and the whole header
// section, from the message as the sender transmits it.
func sentSubject(t *testing.T, msg []byte) (subject, headers string) {
	t.Helper()
	headers, _, _ = strings.Cut(string(msg), "\r\n\r\n")
	for _, line := range strings.Split(headers, "\r\n") {
		if s, ok := strings.CutPrefix(line, "Subject: "); ok {
			return s, headers
		}
	}
	t.Fatalf("no Subject header in:\n%s", headers)
	return "", headers
}

// covers: MA-1508
// C31 (docs/design-per-mailbox-archives.md §4 "Alert subjects"): an archive folder named for its
// mailbox can put letters outside ASCII ("Søren") into an alert email's subject, which names the
// archive by its path. A subject holding any byte outside ASCII is sent as RFC 2047 encoded words
// (mime.QEncoding), so its header line is ASCII only, and a mail program reads it back as written.
// Positive twin first: an ASCII subject is unchanged, byte for byte. A line break in the path still
// adds no header once encoded.
func TestAnAlertSubjectWithLettersOutsideASCIIIsEncoded(t *testing.T) {
	c := Config{From: "backup@example.org", To: "owner@example.org"}

	// Positive twin: an ASCII subject is written as it is.
	ascii, _ := sentSubject(t, c.Message(Event{Result: ResultFailed, Archive: "/vol/StAugustine"}))
	if ascii != "MailArchive backup FAILED: /vol/StAugustine" {
		t.Fatalf("positive twin: an ASCII subject changed: %q", ascii)
	}

	dec := new(mime.WordDecoder)
	for _, tc := range []struct {
		ev   Event
		want string
	}{
		{Event{Result: ResultFailed, Archive: `D:\Mail archives\søren.qz@example.org`}, `MailArchive backup FAILED: D:\Mail archives\søren.qz@example.org`},
		{Event{Result: ResultRecovered, Archive: "/Volumes/Backup/Ærøskøbing Mail"}, "MailArchive backups are working again: /Volumes/Backup/Ærøskøbing Mail"},
		{Event{Result: ResultPaused, Archive: "/m/qz日本語"}, "MailArchive backup PAUSED after repeated failures: /m/qz日本語"},
		// Today's subjects with a dash outside ASCII are encoded too (they went out raw before).
		{Event{Result: ResultTBBehind, Archive: "/Volumes/Backup/Mail", TB: &TBBehind{Accounts: 1}}, "MailArchive: Thunderbird still downloading (backups OK) — Mail"},
		{Event{Result: ResultSyncStale, Archive: "/a/Søren", Sync: &SyncStale{Phrase: "p", Remedy: "r"}}, "MailArchive: Thunderbird not synced for 3 days — /a/Søren"},
	} {
		raw, headers := sentSubject(t, c.Message(tc.ev))
		for i := 0; i < len(headers); i++ {
			if headers[i] >= 0x80 {
				t.Errorf("%s: a byte outside ASCII (0x%02x) in the headers:\n%s", tc.want, headers[i], headers)
				break
			}
		}
		if !strings.HasPrefix(raw, "=?utf-8?q?") {
			t.Errorf("%s: the subject is not RFC 2047 Q-encoded words: %q", tc.want, raw)
		}
		got, err := dec.DecodeHeader(raw)
		if err != nil || got != tc.want {
			t.Errorf("the subject reads back as %q (%v), want %q", got, err, tc.want)
		}
	}

	// A line break in a path holding such letters still adds no header: it is a space, then encoded.
	evil := c.Message(Event{Result: ResultFailed, Archive: "/a/Søren\r\nBcc: evil@example.org"})
	raw, headers := sentSubject(t, evil)
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Errorf("a crafted path added a header line %q", line)
		}
	}
	if got, err := dec.DecodeHeader(raw); err != nil || got != "MailArchive backup FAILED: /a/Søren  Bcc: evil@example.org" {
		t.Errorf("the crafted subject reads back as %q (%v)", got, err)
	}
}
