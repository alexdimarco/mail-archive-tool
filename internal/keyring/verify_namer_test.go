package keyring

import (
	"context"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-851, R37
// verify -full on a real encrypted archive names a damaged message by its sender too,
// read with the key from its row in the sealed search index (B19): "from Bob
// <bob@example.com>" or "from Carol <carol@example.com>" beside its date and subject.
// The positive twin: a whole archive names no message at all.
func TestVerifyFullNamesADamagedMessagesSender(t *testing.T) {
	out, u := fullFixture(t)
	rep, err := VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	if err != nil || rep.Outcome != state.OutcomeIntact || strings.Contains(strings.Join(app.VerifySummary(rep), "\n"), "from ") {
		t.Fatalf("positive twin: %v %+v", err, rep)
	}
	unit := aUnitFile(t, out)
	if err := os.Remove(unit); err != nil {
		t.Fatal(err)
	}
	rep, err = VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	summary := strings.Join(app.VerifySummary(rep), "\n")
	assure.Reached(t, err == nil && rep.Outcome == state.OutcomeDamaged &&
		(strings.Contains(summary, "from Bob <bob@example.com>") || strings.Contains(summary, "from Carol <carol@example.com>")) &&
		strings.Contains(summary, "dated 2024-01-0"),
		"the damaged message named by its sender, read with the key: "+summary)
}
