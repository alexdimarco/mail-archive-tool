package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-327, MA-348, R1, R7, R19
// F7 of the MF-S2 friction review (cell IN-1). The archive's README.txt tells someone with no tool
// that a fillable or terminal row of the report "is missing from this archive" when nobody runs the
// tool again. That is untrue of the rows MF-S2 adds for a part the archive HOLDS: a file ending
// .undecoded was kept exactly as the message carried it, remaining-parts.undecoded is the unread
// rest of a message, a file ending .partial is the readable start of a cut part. The README names
// those files, says the rows describe parts that are in the archive, and says what the other
// new kinds mean, in one paragraph beside the sentence (the sentence itself stays, as
// TestReadmeExplainsGapClasses pins it).
func TestReadmeNamesTheKeptAndCutParts(t *testing.T) {
	text := strings.Join(strings.Fields(archiveReadme), " ")
	if !strings.Contains(text, "every fillable or terminal row is missing from this archive.") {
		t.Fatal("positive twin: the README lost the sentence TestReadmeExplainsGapClasses pins")
	}
	for _, want := range []string{
		"The one exception to that last sentence",
		"names a part that IS in this archive",
		"A file ending .undecoded in a message's -attachments.zip was kept exactly as the message carried it",
		"remaining-parts.undecoded",
		"A file ending .partial holds the readable start of an attachment that was cut short or damaged",
		"unsplit-multipart",
		"fidelity-fallback",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("README.txt lacks %q", want)
		}
	}
}

// covers: MA-348, R1, R7
// F7 of the MF-S2 friction review. The product README no longer says a message shown as one text
// (`unsplit-multipart`) is kept in the attachments zip, says a cut message text has no .partial
// file, and says where the original is when a row ends "the original is kept" (F14).
func TestProductReadmeSaysWhereKeptPartsAre(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := strings.Join(strings.Fields(string(b)), " ")
	for _, want := range []string{
		"`unsplit-multipart` (no zip: the text is on the page as one body)",
		"`fidelity-fallback` and `identity-stub`, which have no part and no zip",
		"a cut message text has no `.partial` file",
		"the original is kept in the source",
		"the .eml beside the page",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md lacks %q", want)
		}
	}
	if strings.Contains(readme, "`corrupt-part`, `unsplit-multipart` and `structure-limit` report kinds)") {
		t.Error("README.md still lists `unsplit-multipart` among the kinds kept in the attachments zip")
	}
}
