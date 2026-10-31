package app

import (
	"strings"
	"testing"
)

// covers: MA-940, R7
// The README.txt an archive carries matches this version (the wording pass before 1.5.2).
// Positive twin first: it still explains itself to someone with no tool (index.html, no
// software needed, the status remedy). It names the program "MailArchive" in prose (the
// lower-case name only inside commands); says a .eml original is there when the archive
// keeps originals, naming the Desktop's own setting beside -raw; names MailArchive
// Desktop's way to search and to go back in time (not only `serve`); and lists the
// housekeeping files MailArchive now writes into the archive's top folder. The encrypted
// note names the Desktop's real button for putting key files back, from one constant
// (RestoreKeyWords) the Desktop's button is checked against.
func TestTheArchiveReadmeMatchesThisVersion(t *testing.T) {
	text := strings.Join(strings.Fields(archiveReadme), " ")
	for _, want := range []string{"open index.html in any web browser", `"mailarchive status -out ."`} {
		if !strings.Contains(text, want) {
			t.Fatalf("positive twin: README.txt lost %q", want)
		}
	}
	for _, want := range []string{
		"This folder is an email archive made by MailArchive.",
		"A message may also have a .eml file beside it: that is the original, unchanged message. It is there only when the archive keeps originals (the MailArchive Desktop setting \"Keep each message's original .eml too\", or -raw at a terminal).",
		"Searching: in MailArchive Desktop, choose \"Open an archive folder…\" and pick this folder (or click Open archive if it is already your archive) for a search page over every message and attachment name. At a terminal: mailarchive serve -out <this folder> (the same search page) mailarchive search -out <this folder> words...",
		"Going back in time: for an archive of a Microsoft 365 mailbox, MailArchive can show the folders and messages as they stood on a past date, including mail later deleted from an ordinary folder. In MailArchive Desktop click \"Go back in time\"; at a terminal run \"mailarchive serve -out <this folder>\" and follow its \"Go back in time\" link.",
		".mailarchive-runhistory.json the recent backup runs (shown as Recent backups in MailArchive Desktop)",
		".mailarchive-sourcestreak.json since when the mail source could not be reached, if it could not",
		".mailarchive-syncstreak.json how many scheduled backups in a row went without a Thunderbird sync, or with no new mail arriving (only for a Thunderbird archive that syncs; used for the 3-day warnings)",
		".mailarchive-paused.json present only while the archive is paused: nothing writes to it until it is resumed in MailArchive Desktop; do not delete it by hand",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("README.txt lacks %q", want)
		}
	}
	for _, bad := range []string{"written by mailarchive", "With the mailarchive program", "an older mailarchive", "offered only by", "If you have the mailarchive program"} {
		if strings.Contains(text, bad) {
			t.Errorf("README.txt still says %q", bad)
		}
	}

	n := EncryptedNote()
	if RestoreKeyWords != "Choose the recovery file and put back the key file" || !strings.Contains(n, `"`+RestoreKeyWords+`"`) || strings.Contains(n, `"Put back the key file"`) {
		t.Errorf("the encrypted note's restore words: %q\n%s", RestoreKeyWords, n)
	}
	if !strings.Contains(n, "mailarchive restore-key-file -out <this folder> -from <the recovery file>") {
		t.Errorf("the encrypted note's restore command:\n%s", n)
	}
}
