package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// covers: MA-1033, R36, S41
// The delete panel's line on Remove from list, Delete and Forget says them in plain words (the
// 1.5.3 friction review's wording item 49): "Remove from list takes this archive off the list and
// keeps the folder. Delete erases its files.", and only for an encrypted archive "Forget this
// encrypted archive here makes this computer forget its record of this archive and the key it
// remembers." — never "forgets the row", never "protecting". Positive twin first: a plain
// archive's line names no Forget.
func TestTheDeletePanelSaysRemoveDeleteAndForgetPlainly(t *testing.T) {
	const plain = "Remove from list takes this archive off the list and keeps the folder. Delete erases its files."
	if got := planView(&archdelete.DeletePlan{Kind: archdelete.Plain})["threeLine"]; got != plain {
		t.Fatalf("positive twin: a plain archive's line is %q, want %q", got, plain)
	}
	got := planView(&archdelete.DeletePlan{Kind: archdelete.Encrypted})["threeLine"]
	if want := plain + " Forget this encrypted archive here makes this computer forget its record of this archive and the key it remembers."; got != want {
		t.Fatalf("an encrypted archive's line is %q, want %q", got, want)
	}
	for _, k := range []archdelete.Kind{archdelete.Plain, archdelete.Encrypted} {
		s := planView(&archdelete.DeletePlan{Kind: k})["threeLine"].(string)
		if strings.Contains(s, "the row") || strings.Contains(s, "protecting") {
			t.Errorf("%v: the line still says jargon: %q", k, s)
		}
	}
}

// covers: MA-1033, S41
// The panel names the plan's sources as "Mail folders: Inbox, Sent" (wording item 50, as the
// terminal's "mail folder 1: Inbox"), and a delete's result is headed "Result for FOLDER"
// (item 57: "Delete of FOLDER" read as a heading for a delete still to come). Run on the page
// script under Node.
func TestThePanelNamesMailFoldersAndTheResultItsFolder(t *testing.T) {
	if passed := runPageScenario(t, "words153"); len(passed) != 2 {
		t.Errorf("the words153 scenario passed %d of its 2 checks: %v", len(passed), passed)
	}
}

// covers: MA-1033, R37
// Forget this encrypted archive here says what forgetting a path changes in the command line's
// words, without "protection" (wording item 81, B18's Desktop side): "Another copy of this archive
// at this same folder path, … is forgotten here too: MailArchive will no longer stop backups into
// that path that lack the archive's key.", and forgetting the whole archive ends naming it and its
// folder: "This computer forgot the encrypted archive ID, last recorded at FOLDER.". Positive twin
// first: the plan still says which folder this computer recorded.
func TestForgetOnTheDesktopSaysWhatItChangesAndNamesTheFolder(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	gone := recordedArchive(t, t.TempDir(), "Gone")
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(gone, name))
	}
	e := newKeysEnv(t, gone)
	_, m := e.post(t, "api/encryption/forget/plan", map[string]any{"path": gone})
	lines := strings.Join(toStrings(m["lines"]), "\n")
	if m["ok"] != true || !strings.Contains(lines, "This computer recorded an encrypted archive at "+gone+".") {
		t.Fatalf("positive twin: the plan: %v", m)
	}
	if !strings.Contains(lines, "Another copy of this archive at this same folder path, such as another drive mounted at the same place, is forgotten here too: MailArchive will no longer stop backups into that path that lack the archive's key.") ||
		strings.Contains(lines, "protection") {
		t.Errorf("the plan's words on another copy at the same path: %s", lines)
	}
	rec, _ := encrecord.Load()
	var id string
	for k := range rec.Archives {
		id = k
	}
	_, m = e.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "whole": true})
	if got := strings.Join(toStrings(m["lines"]), "\n"); m["ok"] != true || got != "This computer forgot the encrypted archive "+id+", last recorded at "+gone+"." {
		t.Errorf("forgetting the whole archive says %q (%v)", got, m)
	}
}
