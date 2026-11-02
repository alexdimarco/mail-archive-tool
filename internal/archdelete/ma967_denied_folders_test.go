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
