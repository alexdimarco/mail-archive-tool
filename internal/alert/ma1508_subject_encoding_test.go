package alert

import (
	"mime"
	"strings"
	"testing"
)

// sentSubject is the message's Subject header, without "Subject: ", and the whole header section,
// from the message as the sender transmits it. Amended 2026-11-25 (the adversarial pass's OUT-01;
// nothing weakened): the header is unfolded as a mail program unfolds it (RFC 5322 §2.2.3: a CRLF
// before a space or tab is taken away), since an encoded subject is now folded over several lines;
// it read only the first line before.
func sentSubject(t *testing.T, msg []byte) (subject, headers string) {
	t.Helper()
	headers, _, _ = strings.Cut(string(msg), "\r\n\r\n")
	lines := strings.Split(headers, "\r\n")
	for i, line := range lines {
		if s, ok := strings.CutPrefix(line, "Subject: "); ok {
			for _, more := range lines[i+1:] {
				if !strings.HasPrefix(more, " ") && !strings.HasPrefix(more, "\t") {
					break
				}
				s += more
			}
			return s, headers
		}
	}
	t.Fatalf("no Subject header in:\n%s", headers)
	return "", headers
}

// covers: MA-1508
// An encoded alert subject is folded into short lines (the adversarial pass's OUT-01; the lead's
// fold: "The encoded Subject is folded into lines of at most 78 octets"). mime.QEncoding joined its
// encoded words with plain spaces on one line: a folder named by 60 emoji under a long parent made a
// Subject line of 1,022 octets, over the 998 a mail server may take, so the failure email could be
// refused; one accent made 103 octets, over the 78 the standards recommend. Positive twin first: an
// ASCII subject is still one line, byte for byte. Each subject holding letters outside ASCII is now
// encoded words of whole characters, each at most 75 octets, folded with CRLF and a space, so every
// header line is at most 78 octets ("Subject: " included), and it reads back as written once unfolded.
func TestAnEncodedAlertSubjectIsFoldedIntoShortLines(t *testing.T) {
	c := Config{From: "backup@example.org", To: "owner@example.org"}
	if got := string(c.Message(Event{Result: ResultFailed, Archive: "/vol/StAugustine"})); !strings.Contains(got, "\r\nSubject: MailArchive backup FAILED: /vol/StAugustine\r\nMIME-Version: 1.0\r\n") {
		t.Fatalf("positive twin: an ASCII subject is not one line as written:\n%s", got)
	}
	dec := new(mime.WordDecoder)
	for _, tc := range []struct {
		what string
		ev   Event
		want string
	}{
		{"60 emoji under a long parent", Event{Result: ResultFailed, Archive: "/Volumes/Family backup drive/Mail archives kept here for the whole family since 2019/" + strings.Repeat("\U0001F600", 60)}, "MailArchive backup FAILED: /Volumes/Family backup drive/Mail archives kept here for the whole family since 2019/" + strings.Repeat("\U0001F600", 60)},
		{"60 CJK letters", Event{Result: ResultFailed, Archive: `D:\Mail archives\` + strings.Repeat("\u65e5", 60)}, `MailArchive backup FAILED: D:\Mail archives\` + strings.Repeat("\u65e5", 60)},
		{"today's Thunderbird subject", Event{Result: ResultTBBehind, Archive: "/Volumes/Backup/Mail", TB: &TBBehind{Accounts: 1}}, "MailArchive: Thunderbird still downloading (backups OK) — Mail"},
		{"one accent", Event{Result: ResultFailed, Archive: `D:\Mail archives\søren.qz@example.org`}, `MailArchive backup FAILED: D:\Mail archives\søren.qz@example.org`},
	} {
		msg := c.Message(tc.ev)
		headers, _, _ := strings.Cut(string(msg), "\r\n\r\n")
		for _, line := range strings.Split(headers, "\r\n") {
			if len(line) > subjectLineMax {
				t.Errorf("%s: a header line of %d octets, over %d: %q", tc.what, len(line), subjectLineMax, line)
			}
		}
		raw, _ := sentSubject(t, msg)
		for _, w := range strings.Fields(raw) {
			if len(w) > encodedWordMax || !strings.HasPrefix(w, "=?utf-8?q?") || !strings.HasSuffix(w, "?=") {
				t.Errorf("%s: an encoded word of %d octets, or not one: %q", tc.what, len(w), w)
			}
		}
		if got, err := dec.DecodeHeader(raw); err != nil || got != tc.want {
			t.Errorf("%s: reads back as %q (%v), want %q", tc.what, got, err, tc.want)
		}
	}
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
