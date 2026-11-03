package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/encmark"
)

// covers: MA-1049, R36
// The posture lines for the delete marker's three cases the slice-2 pass's LOW-4 named (lead's
// words, owner-delegated 2026-11-02), on both surfaces, each RED and the whole of the posture:
// (a) the marker with this computer's deletion log unreadable says the log could not be read and
// why — never "has no record of it"; (b) a name at the marker's that is not a regular file says
// MailArchive did not write it and it is removed by hand — never pointing to a Delete that refuses
// it, and with the log knowing the delete, removing it comes before Delete; (c) a lookup that
// failed, the log not answering, says MailArchive could not check the folder (status said
// nothing), and status refuses a folder it cannot see an archive in with that line, not "run an
// export into it first". Positive twin first: the marker-only line with a log that was read.
func TestMA1049_TheMarkersStatusLinesSayWhatHappened(t *testing.T) {
	now := time.Now()
	out := t.TempDir()
	cmd := encmark.CommandPath(out)
	base := Input{Out: out, HasManifest: true, HasManifestFile: true, Messages: 3}
	only := func(in Input, code, want string) {
		t.Helper()
		rep := Assess(in, now)
		if rep.Posture != "RED" || len(rep.Reasons) != 1 || rep.Reasons[0] != "RED: "+want || len(rep.Codes) != 1 || rep.Codes[0] != code {
			t.Errorf("surface %q: reasons %q (codes %q), want only %q", in.Surface, rep.Reasons, rep.Codes, want)
		}
	}

	// Positive twin: the log was read and has no line.
	in := base
	in.DeleteUnfinished, in.DeleteMarkerOnly, in.DeleteBegun = true, true, "2026-11-02 10:04 -05:00"
	only(in, "delete_unfinished", "a delete of this archive begun 2026-11-02 10:04 -05:00 did not finish, and this computer's deletion log has no record of it (it was begun on another computer or user account) — run mailarchive delete-archive -out "+cmd+", or click Delete… on its row in MailArchive Desktop, to finish it.")

	// (a) The log could not be read.
	in.DeleteLogUnreadable = "CAUSE"
	only(in, "delete_unfinished", "a delete of this archive begun 2026-11-02 10:04 -05:00 did not finish, and MailArchive could not read this computer's deletion log (CAUSE) — check that file, then run mailarchive delete-archive -out "+cmd+", or click Delete… on its row in MailArchive Desktop, to finish it.")
	in.Surface = "desktop"
	only(in, "delete_unfinished", "a delete of this archive begun 2026-11-02 10:04 -05:00 did not finish, and MailArchive could not read this computer's deletion log (CAUSE) — check that file, then click Delete… on its row in Your archives to finish it.")

	// (b) Not a regular file: the marker alone, then with the log knowing the delete.
	notRegular := "this archive's folder holds .mailarchive-deleting.json, which MailArchive did not write (it is a link, a folder or a device), so nothing is backed up into it — if no delete of this archive is under way, remove it by hand."
	for _, surface := range []string{"", "desktop"} {
		in := base
		in.Surface = surface
		in.DeleteUnfinished, in.DeleteMarkerOnly, in.DeleteMarkerNotRegular, in.DeleteLogUnreadable = true, true, true, "CAUSE"
		only(in, "delete_unfinished", notRegular)
	}
	in = base
	in.DeleteUnfinished, in.DeleteMarkerNotRegular = true, true
	only(in, "delete_unfinished", "a delete of this archive did not finish, and its folder holds .mailarchive-deleting.json, which MailArchive did not write (it is a link, a folder or a device) — remove it by hand, then run mailarchive delete-archive -out "+cmd+" again, or click Delete… on its row in MailArchive Desktop, to finish the delete.")
	in.Surface = "desktop"
	only(in, "delete_unfinished", "a delete of this archive did not finish, and its folder holds .mailarchive-deleting.json, which MailArchive did not write (it is a link, a folder or a device) — remove it by hand, then click Delete… on its row in Your archives to finish the delete.")

	// (c) The lookup failed and the log does not answer.
	couldNot := "MailArchive could not check this archive's folder for a delete that did not finish (CAUSE), so nothing is backed up or changed in it — check that the drive or folder can be read."
	for _, surface := range []string{"", "desktop"} {
		in := base
		in.Surface = surface
		in.DeleteCouldNotCheck = "CAUSE"
		only(in, "delete_could_not_check", couldNot)
		if sum := strings.Join(Summary(in, Assess(in, now)), "\n"); !strings.Contains(sum, "  RED: "+couldNot) {
			t.Errorf("surface %q: the summary lacks the line:\n%s", surface, sum)
		}
	}
	// status's refusal of a folder it sees no archive in (a folder that cannot be searched).
	if err := NoArchive(Input{Out: out}); err == nil || !strings.Contains(err.Error(), "run an export into it first") {
		t.Fatalf("positive twin: NoArchive: %v", err)
	}
	err := NoArchive(Input{Out: out, DeleteCouldNotCheck: "CAUSE"})
	if err == nil || err.Error() != "cannot report on "+out+": "+couldNot {
		t.Fatalf("NoArchive with the lookup failed: %v", err)
	}
}
