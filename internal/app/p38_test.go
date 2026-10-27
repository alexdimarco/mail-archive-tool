package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-849, R37
// status of an encrypted archive this computer never recorded (a copied or inherited
// one) whose two key files are gone is RED, naming restore-key-file — nobody can open
// it (P3-8 friction review, B5). Positive twin: with its key files, not RED for that.
func TestStatusIsRedWhenAnUnrecordedArchivesKeyFilesAreGone(t *testing.T) {
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesUnset)
	before := health.Assess(health.Gather(out, ""), time.Now())
	os.Remove(filepath.Join(out, vault.FileName))
	os.Remove(filepath.Join(out, vault.BackupName))
	after := health.Assess(health.Gather(out, ""), time.Now())
	assure.Reached(t, before.Posture != "RED" && after.Posture == "RED" && strings.Contains(strings.Join(after.Reasons, " "), "restore-key-file"),
		"before "+before.Posture+", after "+after.Posture+": "+strings.Join(after.Reasons, " | "))
}

// covers: MA-849, R37
// A changed copy of this computer's own program is replaced and TOLD, never silently
// (program design line 230, §4.2; P3-8 friction review, B3): the backup warns, its
// outcome is "changed found", and status warns that a copy was changed. The copy on
// the drive is this program's again.
func TestAChangedSelfCopyIsReplacedAndTold(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	_, name := selfPlatformAndName()
	p := filepath.Join(out, container.ProgramDir, name)
	good, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), good...)
	bad[len(bad)/2] ^= 0xff // same size, other bytes
	os.WriteFile(p, bad, 0o755)
	res, logged := backup(t, out, key, ProgramCopiesUnset)
	now, _ := os.ReadFile(p)
	lr, _, _ := state.ReadLastRun(out)
	assure.Reached(t, res.Program.Class == ProgramChangedFound && strings.Contains(logged, "was changed; it was replaced with this computer's own program") &&
		lr.Program == state.ProgramChanged && state.ProgramWarns(lr.Program) && bytes.Equal(now, good),
		"told: "+res.Program.Class+"\n"+logged)
}
