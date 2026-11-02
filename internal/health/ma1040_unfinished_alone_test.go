package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/encmark"
)

// covers: MA-1040, R18
// When a delete of the archive did not finish, status says that alone, in a sentence with its
// period, and leaves out every line that would mend or keep the archive — rebuild its index,
// baseline its fixity, keep originals with -raw, its go-back history (the 1.5.3 re-walk's N8, N20).
// Positive twin first: the same archive with no delete under way gets that advice.
func TestADeleteNotFinishedIsTheWholePosture(t *testing.T) {
	now := time.Now()
	in := Input{Out: t.TempDir(), HasManifest: true, HasManifestFile: true, Messages: 1800,
		Extractable: Extractable{WithEML: 0, Total: 1800}}
	advice := []string{"reindex", "-raw", "Extractable:", "History:", "Fixity coverage:", "verify -record"}

	// Positive twin: no delete under way.
	twin := Assess(in, now)
	tsum := strings.Join(Summary(in, twin), "\n")
	if !twin.has("index_missing") || !strings.Contains(tsum, "Extractable:") || !strings.Contains(tsum, "History:") {
		t.Fatalf("the twin:\n%s", tsum)
	}

	in.DeleteUnfinished = true
	rep := Assess(in, now)
	want := "RED: a delete of this archive did not finish — run mailarchive delete-archive -out " + encmark.CommandPath(in.Out) + " again, or click Delete… on its row in MailArchive Desktop, to finish it."
	if rep.Posture != "RED" || len(rep.Reasons) != 1 || rep.Reasons[0] != want || len(rep.Codes) != 1 || rep.Codes[0] != "delete_unfinished" {
		t.Fatalf("reasons %q (codes %q), want only %q", rep.Reasons, rep.Codes, want)
	}
	sum := strings.Join(Summary(in, rep), "\n")
	for _, a := range advice {
		if strings.Contains(sum, a) {
			t.Errorf("the summary still says %q:\n%s", a, sum)
		}
	}
	if !strings.Contains(sum, "Messages:   1800 in manifest") || !strings.Contains(sum, "  "+want) {
		t.Errorf("the summary lost its facts or its line:\n%s", sum)
	}

	// The Desktop's words end with their period too.
	in.Surface = "desktop"
	if got := Assess(in, now).Reasons; len(got) != 1 || got[0] != "RED: a delete of this archive did not finish — click Delete… on its row in Your archives to finish it." {
		t.Fatalf("the Desktop's reasons: %q", got)
	}
}
