package archdelete

import (
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1216, R45, S82, S83
// Archive delete is one of the four places that say a newer format's words (fold check 6's S7;
// docs/design-gmail-dedup.md §7.4): a version-8 archive (a newer MailArchive part-way through
// tidying it) is refused with the tidy-up words, naming version 8, and nothing is removed; a
// version-9 archive with "needs a newer MailArchive" and the update remedy. Positive twin first:
// a version-7 archive, which this build reads, is planned, not refused as newer.
func TestArchiveDeleteSaysTheTidyUpWordsForVersion8(t *testing.T) {
	twin := t.TempDir()
	writeManifest(t, twin, 7, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}})
	mustPlan(t, twin, testDeps())

	v8 := t.TempDir()
	writeManifest(t, v8, 8, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}})
	rc, msg, opts := refusedCase(t, v8, testDeps(), CaseNewerFormat, state.TidyUpWords, "manifest format 8", "understands 7")
	assure.Refused(t, rc, msg, append(opts, assure.Forbid("Update MailArchive, then delete it."))...)

	v9 := t.TempDir()
	writeManifest(t, v9, 9, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}})
	rc, msg, opts = refusedCase(t, v9, testDeps(), CaseNewerFormat, state.NewerWords, "manifest format 9", "Update MailArchive, then delete it.")
	assure.Refused(t, rc, msg, append(opts, assure.Forbid(state.TidyUpWords))...)
}
