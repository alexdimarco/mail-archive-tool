package health

import (
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1297, R45, S82
// `status` says the tidy-up words for an archive a newer MailArchive is tidying (version 8) as a
// WARN of its own, never "RED: the archive's manifest cannot be read" (the B3 friction review's
// F9, with the builder's call on the posture: nothing is wrong with the archive, and the step is
// on the other computer). Positive twin first: a manifest of any other newer version, and a
// damaged one, stay RED "cannot be read".
func TestStatusSaysTheTidyUpWordsAsAWarnOfItsOwn(t *testing.T) {
	at := func(err error) Report {
		return Assess(Input{Out: t.TempDir(), HasManifestFile: true, ManifestLoadErr: err, LastRunState: state.LastRunAbsent}, time.Now())
	}
	nine := at(&state.NewerFormatError{Name: "m", Version: 9, Understood: 7})
	if nine.Posture != "RED" || !slices.Contains(nine.Codes, "manifest_unreadable") {
		t.Fatalf("positive twin: version 9: %s %v", nine.Posture, nine.Codes)
	}
	eight := at(&state.NewerFormatError{Name: "m", Version: 8, Understood: 7})
	if slices.Contains(eight.Codes, "manifest_unreadable") || !slices.Contains(eight.Codes, "manifest_tidying") {
		t.Fatalf("version 8: codes %v", eight.Codes)
	}
	joined := strings.Join(eight.Reasons, "\n")
	if !strings.Contains(joined, "WARN: "+state.TidyUpWords) || strings.Contains(joined, "cannot be read") {
		t.Fatalf("version 8's reasons:\n%s", joined)
	}
}
