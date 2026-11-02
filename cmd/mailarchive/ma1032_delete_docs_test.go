package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docSection is the text of the file at rel (from the module root) from the line starting with
// head up to the next heading of the same level; "" when there is no such line.
func docSection(t *testing.T, rel, head string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	i := strings.Index(text, "\n"+head)
	if i < 0 {
		return ""
	}
	sec := text[i+1:]
	level := head[:strings.Index(head, " ")+1] // "## " or "### "
	if j := strings.Index(sec[len(head):], "\n"+level); j >= 0 {
		sec = sec[:len(head)+j]
	}
	return sec
}

// deleteDocControls are the MailArchive Desktop controls the delete docs may name in bold, each
// with the source file and the words that draw it.
var deleteDocControls = map[string][2]string{
	"Delete…":                                {"internal/desktop/dashboard.go", ">Delete…</button>"},
	"Remove from list":                       {"internal/desktop/dashboard.go", ">Remove from list</button>"},
	"Your archives":                          {"internal/desktop/dashboard.go", "<h2>Your archives</h2>"},
	"This archive":                           {"internal/desktop/dashboard.go", "<h2>This archive</h2>"},
	"Open an archive folder…":                {"internal/desktop/dashboard.go", ">Open an archive folder…</button>"},
	"Switch to this archive":                 {"internal/desktop/dashboard.go", "Switch to this archive</button>"},
	"Delete not finished: click Delete again": {"internal/desktop/dashboard.go", `<span class="archive-tag">Delete not finished: click Delete again</span>`},
	"Save this plan":                         {"internal/desktop/deletescript.go", "delButton('Save this plan'"},
	"Delete permanently":                     {"internal/desktop/deletescript.go", "delButton('Delete permanently'"},
	"Save deletion record":                   {"internal/desktop/deletescript.go", "delButton('Save deletion record'"},
	"Dismiss":                                {"internal/desktop/deletescript.go", "delButton('Dismiss'"},
	// Amended 2026-11-02 (the uninstall-docs group's adversarial pass, finding 6): "Delete not
	// finished" now says these start nothing, in place of "not backed up".
	"Archive now": {"internal/desktop/dashboard.go", `{{end}}>Archive now</button>`},
	"Run now":     {"internal/desktop/dashboard.go", `>Run now</button>`},
}

var (
	boldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	// cmdRe is a mailarchive command the docs give: the verb and what follows it on the line, up
	// to a closing backquote or a comment.
	cmdRe  = regexp.MustCompile("mailarchive ([a-z][a-z-]*)([^`#\n]*)")
	flagRe = regexp.MustCompile(`(?:^|\s)(-[a-z][a-z-]*)`)
)

// covers: MA-1032, X11
// The README's "Deleting an archive" and the 1.5.3 release notes say what 1.5.3's delete does
// (the 1.5.3 friction review's B16): Delete… and Remove from list on the Desktop; delete-archive,
// unlist-archive and add-archive at the terminal; "Delete not finished" and how to finish it;
// where deletions.log is on Windows, macOS and Linux; that after deleting the current archive you
// switch to another; and uninstall -yes -log-copied. Every control they name in bold is drawn by
// the Desktop in those words, and every mailarchive command they give is a verb the program runs,
// with flags its -h lists. Positive twin first: the README's Uninstall bullet keeps its promise in
// the new words.
func TestTheDeleteDocsSayWhat153DoesAndNameRealControls(t *testing.T) {
	desktop := docSection(t, "README.md", "## MailArchive Desktop (the dashboard)")
	if !strings.Contains(desktop, "uninstalling never deletes your mail (only **Delete…** does)") || strings.Contains(desktop, "your mail is never deleted") {
		t.Fatalf("positive twin: the README's Uninstall bullet says uninstalling never deletes your mail:\n%s", desktop)
	}

	readme := docSection(t, "README.md", "## Deleting an archive")
	notes := docSection(t, "packaging/release-notes.md", "### New in 1.5.3")
	for name, sec := range map[string]string{"README.md's Deleting an archive": readme, "the 1.5.3 release notes": notes} {
		if sec == "" {
			t.Fatalf("%s is missing", name)
		}
		flat := strings.Join(strings.Fields(sec), " ")
		for _, want := range []string{
			"**Delete…**", "**Remove from list**",
			"mailarchive delete-archive -out DIR", "-plan-out", "-record-out", "mailarchive unlist-archive -out DIR", "mailarchive add-archive DIR",
			"Delete not finished: click Delete again", "mailarchive status",
			`%APPDATA%\mailarchive\deletions.log`, "~/Library/Application Support/mailarchive/deletions.log", "~/.config/mailarchive/deletions.log",
			"After deleting the current archive", "Switch to this archive",
			"somewhere you keep records",
		} {
			if !strings.Contains(flat, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
		if name == "the 1.5.3 release notes" && !strings.Contains(flat, "mailarchive uninstall -yes -log-copied") {
			t.Errorf("%s lacks uninstall -yes -log-copied", name)
		}
		// Finding 6: no backup reads deletions.log, so the docs do not promise none runs.
		if strings.Contains(flat, "not backed up") {
			t.Errorf("%s says a delete not finished is not backed up, which nothing enforces", name)
		}
		// Finding 7: on Windows the command line does not find mailarchive.exe by name.
		if name == "README.md's Deleting an archive" && !strings.Contains(flat, `"C:\Program Files\MailArchive Desktop\mailarchive.exe" delete-archive -out DIR`) {
			t.Errorf("%s lacks the Windows full path", name)
		}

		// Every bold name is a heading of the docs' own (ending "." or ":", or the two below) or a
		// control the Desktop draws in those words.
		for _, m := range boldRe.FindAllStringSubmatch(sec, -1) {
			label := strings.Join(strings.Fields(m[1]), " ")
			if strings.HasSuffix(label, ".") || strings.HasSuffix(label, ":") || label == "After deleting the current archive" || label == "At the command line" {
				continue
			}
			c, ok := deleteDocControls[label]
			if !ok {
				t.Errorf("%s names %q in bold, which is not a control this test knows the Desktop draws", name, label)
				continue
			}
			src, err := os.ReadFile(filepath.Join(moduleRoot(t), c[0]))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(src), c[1]) {
				t.Errorf("%s names %q, which %s does not draw", name, label, c[0])
			}
		}
	}

	// Finding 9: uninstall -yes refuses while the log holds any line, not only a delete's.
	for name, sec := range map[string]string{"README.md's Desktop section": desktop, "the 1.5.3 release notes": notes} {
		if flat := strings.Join(strings.Fields(sec), " "); strings.Contains(flat, "lists a delete") || !strings.Contains(flat, "holds any line") {
			t.Errorf("%s must say uninstall -yes refuses while the log holds any line", name)
		}
	}

	// Every command given is a verb the program runs, with flags its -h lists.
	help := map[string]string{}
	for _, sec := range []string{readme, notes, docSection(t, "README.md", "## MailArchive Desktop (the dashboard)")} {
		for _, m := range cmdRe.FindAllStringSubmatch(sec, -1) {
			verb := m[1]
			if verb == "wizard" || verb == "desktop" {
				continue // the README's other bullets; not this change's commands
			}
			if _, ok := help[verb]; !ok {
				out, _ := exec.Command(testBin, verb, "-h").CombinedOutput()
				help[verb] = string(out)
				if !strings.Contains(help[verb], "mailarchive "+verb) {
					t.Errorf("the docs give mailarchive %s, which the program does not run as a verb:\n%s", verb, out)
				}
			}
			for _, f := range flagRe.FindAllStringSubmatch(m[2], -1) {
				if !strings.Contains(help[verb], "\n  "+f[1]+" ") && !strings.Contains(help[verb], "\n  "+f[1]+"\n") {
					t.Errorf("the docs give mailarchive %s %s, a flag its -h does not list:\n%s", verb, f[1], help[verb])
				}
			}
		}
	}
	for _, verb := range []string{"delete-archive", "unlist-archive", "add-archive", "uninstall", "status"} {
		if _, ok := help[verb]; !ok {
			t.Errorf("the docs' commands were not all found: no mailarchive %s", verb)
		}
	}
}
