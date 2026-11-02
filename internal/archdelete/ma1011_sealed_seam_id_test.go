package archdelete

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// covers: MA-1011, R36
// The Sealed seam is asked with the folder and the archive ID the folder's key files claim
// (DeletePlan.ArchiveID), and its answer is what the saved plan states: the Desktop answers only
// for the archive open here that is that archive in that folder, never another folder's whose
// name folds to the same key (the delete residuals' security pass, key-use finding 2). Positive
// twin first: a seam that answers states the values; one that does not leaves them "not known".
func TestTheSealedSeamIsAskedForTheFolderAndItsArchiveID(t *testing.T) {
	vault.UseTestCost()
	v, _, _, err := vault.Create("a long enough test password")
	if err != nil {
		t.Fatal(err)
	}
	data, err := v.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	dir := encryptedArchive(t)
	writeFile(t, filepath.Join(dir, vault.FileName), string(data))
	deps := testDeps()
	var askedFolder, askedID string
	deps.Sealed = func(folder, archiveID string) (Captured, bool) {
		askedFolder, askedID = folder, archiveID
		return Captured{Messages: 7}, archiveID == v.ArchiveID.String()
	}
	p := mustPlan(t, dir, deps)
	text := string(RecordText(RecordInput{Status: StatusPlanned, Plan: p}))
	assure.Reached(t, askedFolder == dir && askedID == v.ArchiveID.String() && strings.Contains(text, "Messages: 7 (as recorded in the archive)\n"),
		"the seam asked for the folder and the ID its key files claim, and its answer stated")

	deps.Sealed = func(string, string) (Captured, bool) { return Captured{Messages: 7}, false }
	text = string(RecordText(RecordInput{Status: StatusPlanned, Plan: mustPlan(t, dir, deps)}))
	assure.Reached(t, strings.Contains(text, "Messages: not known: encrypted, key not open\n") && !strings.Contains(text, "Messages: 7"),
		"a seam that does not answer leaves the sealed values not known")
}
