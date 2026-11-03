package main

import (
	"os/exec"
	"strings"
	"testing"
)

// covers: MA-1052, R36
// `mailarchive add-archive -h` and the README say what slice 4 built (docs/design-archive-delete.md
// §15.4's DM-17; §15.10 slice 5's help item): a folder whose delete did not finish — one holding the
// delete marker, a delete begun on another computer or user account — is added too, with or without
// its archive's files, so delete-archive can finish it, and it does not become the archive MailArchive
// Desktop shows. Positive twin first: the help's sentence on an archive you already have is there.
func TestMA1052_AddArchivesHelpAndTheReadmeSayAMarkerFolderIsAdded(t *testing.T) {
	out, _ := exec.Command(testBin, "add-archive", "-h").CombinedOutput()
	flat := strings.Join(strings.Fields(string(out)), " ")
	if !strings.Contains(flat, "Nothing in the folder is changed, and it does not become the archive MailArchive Desktop shows.") {
		t.Fatalf("positive twin: add-archive -h:\n%s", out)
	}
	if !strings.Contains(flat, "A folder whose delete did not finish (one holding .mailarchive-deleting.json, for example a delete begun on another computer or user account) is added the same way, with or without its archive's files, so delete-archive can finish that delete.") {
		t.Errorf("add-archive -h does not say a folder whose delete did not finish is added:\n%s", out)
	}
	readme := strings.Join(strings.Fields(docSection(t, "README.md", "## Deleting an archive")), " ")
	for _, want := range []string{
		"**A delete begun on another computer.** A shared folder or a drive can hold an archive whose delete was begun on another computer or user account and did not finish (its folder holds `.mailarchive-deleting.json`).",
		"A folder not under **Your archives** is added with **Open an archive folder…** or `mailarchive add-archive DIR`, without becoming the archive MailArchive Desktop shows.",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("the README lacks %q", want)
		}
	}
}
