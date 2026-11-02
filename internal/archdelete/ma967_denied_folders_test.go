package archdelete

import (
	"strings"
	"testing"
)

// covers: MA-967, R36
// A file the system would not let the delete remove (a permission refused) is reported with the
// folder it is in, on macOS and Linux, where removing a file needs leave to change its folder (the
// 1.5.3 friction review's B6, wording item 24: the result named the files, not the read-only
// folder that caused it): "The system did not allow it in the folder Mailbox/Inbox (a locked
// file, a folder you may not change, or a read-only drive): …", several folders each named once,
// and the archive's own folder as "the archive's folder". On Windows, which refuses a file for the
// file itself, the remedy names no folder. Positive twin first: a file in use names no folder.
func TestADeniedRemedyNamesTheFoldersOfTheFilesRefused(t *testing.T) {
	run := func(t *testing.T, fail func(string) error) *Result {
		t.Helper()
		r := newRig(t, plainBesidePersonsFiles(t))
		faultOn(t, nil, fail)
		return r.run(t, false)
	}
	only := func(err func(string) error, rels ...string) func(string) error {
		return func(rel string) error {
			for _, r := range rels {
				if rel == r {
					return err(rel)
				}
			}
			return nil
		}
	}

	// Positive twin: in use, no folder named. (Each case in its own subtest, so its fault ends with it.)
	t.Run("in use", func(t *testing.T) {
		res := run(t, only(inUseErr, "Mailbox/Inbox/index.html"))
		if res.Status != StatusPartial || !strings.HasSuffix(res.Lines[0], "Close Outlook or any program using it, then click Delete again.") || strings.Contains(res.Lines[0], "in the folder") {
			t.Fatalf("positive twin: %s", res.Message())
		}
	})

	for _, c := range []struct {
		name  string
		rels  []string
		where []string // the words naming the folders, each in the remedy (macOS and Linux)
	}{
		{"one folder", []string{"Mailbox/Inbox/index.html", "Mailbox/Inbox/index-2.html"}, []string{"The system did not allow it in the folder Mailbox/Inbox (a locked file"}},
		{"two folders", []string{"Mailbox/Inbox/index.html", "Mailbox/Sent/Deep/index.html"}, []string{"The system did not allow it in the folders ", "Mailbox/Inbox", " and ", "Mailbox/Sent/Deep", " (a locked file"}},
		{"the archive's folder", []string{"search.db"}, []string{"The system did not allow it in the archive's folder (a locked file"}},
		{"both", []string{"search.db", "Mailbox/Inbox/index.html"}, []string{"The system did not allow it in the archive's folder and the folder Mailbox/Inbox (a locked file"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := run(t, only(deniedErr, c.rels...))
			line := res.Lines[0]
			if res.Status != StatusPartial || len(res.NotRemoved) != len(c.rels) {
				t.Fatalf("%s", res.Message())
			}
			if !deniedNamesFolders {
				if strings.Contains(line, " in the folder") || strings.Contains(line, "the archive's folder") || !strings.Contains(line, "Windows security or antivirus blocked it: allow MailArchive or delete ") {
					t.Fatalf("on Windows the remedy names no folder: %s", line)
				}
				return
			}
			for _, w := range c.where {
				if !strings.Contains(line, w) {
					t.Errorf("the remedy lacks %q: %s", w, line)
				}
			}
			if strings.Count(line, "Mailbox/Inbox ") > 1 || strings.Count(line, "Mailbox/Inbox (") > 1 {
				t.Errorf("a folder is named more than once: %s", line)
			}
		})
	}
}

// covers: MA-967, R36
// The words naming the denied folders (deniedFolders) name at most three, then "and N more" for
// the rest, each folder once, and strip a control character from a folder's name, so a name
// cannot write over the result's line (amended 2026-11-02, the uninstall-docs group's adversarial
// pass, finding 5: neither was proven). Files refused for another cause are not counted. Positive
// twin first: three folders are all named, "the folders A, B and C".
func TestTheDeniedFoldersAreCountedPastThreeAndStripped(t *testing.T) {
	denied := func(rels ...string) []LeftFile {
		var l []LeftFile
		for _, r := range rels {
			l = append(l, LeftFile{Rel: r, Cause: CauseDenied})
		}
		return l
	}
	if got, want := deniedFolders(denied("A/1.html", "B/1.html", "C/1.html", "A/2.html")), "the folders A, B and C"; got != want {
		t.Fatalf("positive twin: three folders: %q, want %q", got, want)
	}
	for _, c := range []struct {
		left []LeftFile
		want string
	}{
		{denied("A/1.html", "B/1.html", "C/1.html", "D/1.html"), "the folders A, B, C and 1 more"},
		{denied("A/1.html", "B/1.html", "C/1.html", "D/1.html", "E/1.html", "D/2.html", "search.db"), "the archive's folder and the folders A, B, C and 2 more"},
		{append(denied("A/1.html"), LeftFile{Rel: "Z/1.html", Cause: CauseInUse}), "the folder A"},
		{denied("Mail\x1b[2K\rbox/In\u0085box/1.html"), "the folder Mail[2Kbox/Inbox"},
	} {
		if got := deniedFolders(c.left); got != c.want {
			t.Errorf("deniedFolders of %d files = %q, want %q", len(c.left), got, c.want)
		}
	}
}
