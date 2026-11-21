package app

import (
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1471, R18
// The summaries' last line fits what is known (the 1.7.5 friction review's F4 and F9), as status's
// does: one account Thunderbird still lists is told to let Thunderbird download its mail, and so are
// several when all are listed; an Outlook .pst moved away is told to put it back in its folder or
// choose it in its new place. Positive twin first: an account no longer listed, and a mix, keep "add
// its account back".
func TestTheSummarysRemedyFitsWhatIsKnown(t *testing.T) {
	home := tmpDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	im := filepath.Join(home, ".thunderbird", "p", "ImapMail")
	b, c := filepath.Join(im, "imap.qzb.example"), filepath.Join(im, "imap.qzc.example")
	last := func(s []SkippedInput) string { l := SkippedLines(s, true); return l[len(l)-1] }
	if got := last([]SkippedInput{{Path: b, Name: "qzb@qz.example"}}); got != "If you didn't remove it on purpose, add its account back in Thunderbird." {
		t.Fatalf("positive twin: one not listed: %q", got)
	}
	if got := last([]SkippedInput{{Path: b, Name: "qzb@qz.example", Listed: true}, {Path: c}}); got != "If you didn't remove one on purpose, add its account back in Thunderbird." {
		t.Fatalf("positive twin: a mix keeps the plural: %q", got)
	}
	if got := last([]SkippedInput{{Path: b, Name: "qzb@qz.example", Listed: true}}); got != "Thunderbird still lists this account: open Thunderbird and let it download the account's mail; the next backup finds it again." {
		t.Errorf("F4, one listed: %q", got)
	}
	if got := last([]SkippedInput{{Path: b, Listed: true}, {Path: c, Listed: true}}); got != "Thunderbird still lists these accounts: open Thunderbird and let it download their mail; the next backup finds them again." {
		t.Errorf("F4, all listed: %q", got)
	}
	pst := filepath.Join(home, "Documents", "Outlook Files", "archive 2019.pst")
	if got := last([]SkippedInput{{Path: pst}}); strings.Contains(got, "add its account back") ||
		// Amended 2026-11-20 by the 1.7.5 re-verify (RF-05), word for word at the same strength.
		got != "If you moved this file, put it back in "+filepath.Dir(pst)+" and the next backup finds it again. To back it up from its new place instead, click Make a new archive… in MailArchive Desktop and choose the file there: that makes a separate archive." {
		t.Errorf("F9, a .pst: %q", got)
	}
}
