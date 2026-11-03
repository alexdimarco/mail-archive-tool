package health

import (
	"testing"
	"time"

	"mail-archive-tool/internal/encmark"
)

// covers: MA-1068, R36
// DM-27's posture line (design-archive-delete §15.4; the slice-5 adversarial pass's LOW-4; lead's
// words, owner-delegated 2026-11-02), on both surfaces, RED and the whole of the posture, code
// delete_unfinished: this computer's deletion log has a delete of the folder that wrote its note,
// yet the folder does not hold it — another drive at the same place (a rotation drive), or the note
// removed by hand. The line names both ways out: plug in the drive the delete began on and finish it
// there, or finish it here. Positive twin first: the log's own line, "… run … again".
func TestMA1068_TheDisagreeLineNamesTheDriveTheDeleteBeganOn(t *testing.T) {
	now := time.Now()
	out := t.TempDir()
	cmd := encmark.CommandPath(out)
	only := func(in Input, want string) {
		t.Helper()
		rep := Assess(in, now)
		if rep.Posture != "RED" || len(rep.Reasons) != 1 || rep.Reasons[0] != "RED: "+want || len(rep.Codes) != 1 || rep.Codes[0] != "delete_unfinished" {
			t.Errorf("surface %q: reasons %q (codes %q), want only %q", in.Surface, rep.Reasons, rep.Codes, want)
		}
	}
	in := Input{Out: out, HasManifest: true, HasManifestFile: true, Messages: 3, DeleteUnfinished: true}
	only(in, "a delete of this archive did not finish — run mailarchive delete-archive -out "+cmd+" again, or click Delete… on its row in MailArchive Desktop, to finish it.")

	const head = "a delete this computer began in this folder did not finish, but the folder does not hold the note that delete left — if this is a different drive, plug in the drive the delete began on and finish the delete there; if it is the same drive and the note was removed, "
	in.DeleteDisagrees = true
	only(in, head+"run mailarchive delete-archive -out "+cmd+", or click Delete… on its row in MailArchive Desktop, to finish it.")
	in.Surface = "desktop"
	only(in, head+"click Delete… on its row in Your archives to finish it.")
}
