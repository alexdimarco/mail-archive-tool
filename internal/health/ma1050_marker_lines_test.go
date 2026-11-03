package health

import (
	"testing"
	"time"

	"mail-archive-tool/internal/encmark"
)

// covers: MA-1050, MA-1049, R36
// The delete marker's two posture lines added in slice 5 (design-archive-delete §15.4's DM-10 note,
// §15.10 slice 5; lead's words, owner-delegated 2026-11-02), on both surfaces, each RED and the
// whole of the posture: a delete only the marker knows whose run this computer's deletion log began
// says it was begun on this computer — never "on another computer" — and how to list the folder at
// its new place; one seen with MailArchive's settings folder not found says so and names the remedy —
// never "has no record of it", since no log was asked (the slice-2 adversarial pass's LOW-4, its last
// item). Positive twin first: the marker-only line with a log that was read and does not know the run.
func TestMA1050_TheMarkersBegunHereAndNoSettingsLinesSayWhatHappened(t *testing.T) {
	now := time.Now()
	out := t.TempDir()
	cmd := encmark.CommandPath(out)
	base := Input{Out: out, HasManifest: true, HasManifestFile: true, Messages: 3}
	only := func(in Input, want string) {
		t.Helper()
		rep := Assess(in, now)
		if rep.Posture != "RED" || len(rep.Reasons) != 1 || rep.Reasons[0] != "RED: "+want || len(rep.Codes) != 1 || rep.Codes[0] != "delete_unfinished" {
			t.Errorf("surface %q: reasons %q (codes %q), want only %q", in.Surface, rep.Reasons, rep.Codes, want)
		}
	}
	const begun = "2026-11-02 10:04 -05:00"

	// Positive twin: another computer's delete.
	in := base
	in.DeleteUnfinished, in.DeleteMarkerOnly, in.DeleteBegun = true, true, begun
	only(in, "a delete of this archive begun "+begun+" did not finish, and this computer's deletion log has no record of it (it was begun on another computer or user account) — run mailarchive delete-archive -out "+cmd+", or click Delete… on its row in MailArchive Desktop, to finish it.")

	// Begun here (DM-10).
	in.DeleteBegunHere = true
	only(in, "a delete of this archive begun "+begun+" did not finish; it was begun on this computer, which no longer matches it to this folder (the drive may have been plugged in again under another name) — run mailarchive delete-archive -out "+cmd+" (add it first with mailarchive add-archive if it is not in Your archives), or click Delete… on its row in MailArchive Desktop, to finish it.")
	in.Surface = "desktop"
	only(in, "a delete of this archive begun "+begun+" did not finish; it was begun on this computer, which no longer matches it to this folder (the drive may have been plugged in again under another name) — click Delete… on its row in Your archives to finish it (add the folder first with Open an archive folder… if it is not listed).")

	// No settings folder (LOW-4's last item): no log was asked.
	in = base
	in.DeleteUnfinished, in.DeleteMarkerOnly, in.DeleteNoSettings = true, true, "CAUSE"
	only(in, "a delete of this archive did not finish, and MailArchive could not find its settings folder, which holds this computer's deletion log (CAUSE) — check that your home folder is set (HOME, or AppData on Windows), then run mailarchive delete-archive -out "+cmd+", or click Delete… on its row in MailArchive Desktop, to finish it.")
	in.Surface = "desktop"
	only(in, "a delete of this archive did not finish, and MailArchive could not find its settings folder, which holds this computer's deletion log (CAUSE) — check that your home folder is set (HOME, or AppData on Windows), then click Delete… on its row in Your archives to finish it.")
}
