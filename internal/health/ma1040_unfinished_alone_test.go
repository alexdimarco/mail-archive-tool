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

	// The facts left point at no posture line that is gone, and a delete that removed the manifest
	// is never "no run has completed here" (the settle pass's MED-3). Positive twins first: with no
	// delete under way each still points at the posture, which holds that line.
	for _, c := range []struct {
		name    string
		in      Input
		twinHas string
		want    string
	}{
		{"index unreadable", Input{HasManifest: true, HasManifestFile: true, Messages: 10, HasIndexFile: true},
			"search index unreadable (see the posture below)", "Messages:   10 in manifest · search index unreadable\n"},
		{"index busy", Input{HasManifest: true, HasManifestFile: true, Messages: 10, HasIndexFile: true, IndexBusy: true},
			"search index busy (see the posture below)", "Messages:   10 in manifest · search index busy\n"},
		{"manifest unreadable", Input{HasManifestFile: true},
			"Messages:   (manifest present but unreadable — see the posture below)", "Messages:   (manifest present but unreadable)\n"},
		{"manifest gone", Input{},
			"Messages:   (no manifest yet — no run has completed here)", "Messages:   (not known — a delete of this archive did not finish)\n"},
		{"schedule unreadable", Input{HasManifestFile: true, HasDescriptorFile: true},
			"Schedule:   (descriptor present but unreadable — see the posture below)", "Schedule:   (descriptor present but unreadable)\n"},
		{"verify schedule unreadable", Input{HasManifestFile: true, HasVerifyDescriptorFile: true},
			"Verify schedule: (descriptor present but unreadable — see the posture below)", "Verify schedule: (descriptor present but unreadable)\n"},
	} {
		c.in.Out = t.TempDir()
		tsum := strings.Join(Summary(c.in, Assess(c.in, now)), "\n")
		if !strings.Contains(tsum, c.twinHas) {
			t.Errorf("%s, the twin:\n%s", c.name, tsum)
		}
		c.in.DeleteUnfinished = true
		r := Assess(c.in, now)
		dsum := strings.Join(Summary(c.in, r), "\n") + "\n"
		if len(r.Codes) != 1 || r.Codes[0] != "delete_unfinished" || !strings.Contains(dsum, c.want) ||
			strings.Contains(dsum, "posture below") || strings.Contains(dsum, "no run has completed") {
			t.Errorf("%s, a delete not finished (codes %q), want %q:\n%s", c.name, r.Codes, c.want, dsum)
		}
	}

	// The Desktop's words end with their period too.
	in.Surface = "desktop"
	if got := Assess(in, now).Reasons; len(got) != 1 || got[0] != "RED: a delete of this archive did not finish — click Delete… on its row in Your archives to finish it." {
		t.Fatalf("the Desktop's reasons: %q", got)
	}
}
