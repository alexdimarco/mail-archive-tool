package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// covers: MA-847, R37
// status says, without the key, what the last backup's program step did (program
// design §4, §8; PG4): the outcome from the record's closed list — a note when the
// copies are as they should be, a warning when they are incomplete, a note when a newer
// MailArchive keeps them — and the record beside the archive holds nothing more than
// that code.
func TestStatusSaysWhatTheProgramStepDid(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	// told is status's line for code: "WARN: <text>" as a warning, the text alone as a note.
	told := func(code string) string {
		hr := health.Assess(health.Gather(out, ""), time.Now())
		for _, r := range hr.Reasons {
			if strings.HasSuffix(r, state.ProgramText(code)) {
				return r
			}
		}
		return ""
	}
	updated := told(state.ProgramUpdated)
	_, name := selfPlatformAndName()
	p := filepath.Join(out, container.ProgramDir, name)
	os.Remove(p)
	os.Mkdir(p, 0o755) // something in its place: incomplete
	backup(t, out, key, ProgramCopiesUnset)
	incomplete := told(state.ProgramIncomplete)
	raw, _ := os.ReadFile(filepath.Join(out, state.LastRunName))

	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	s.SetProgramRawForTest([]byte(`{"format":2,"keep":true}`)) // a newer MailArchive's set
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	backup(t, out, key, ProgramCopiesUnset)
	unread := told(state.ProgramUnread)
	assure.Reached(t, updated == state.ProgramText(state.ProgramUpdated) && incomplete == "WARN: "+state.ProgramText(state.ProgramIncomplete) &&
		unread == state.ProgramText(state.ProgramUnread) &&
		strings.Contains(string(raw), state.ProgramIncomplete) && !strings.Contains(string(raw), name),
		fmt.Sprintf("status: %q, %q, %q", updated, incomplete, unread))
}
