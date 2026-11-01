package archdelete

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-969, R36
func TestRemoveFromListClearsABackupForNamingTheFolderItself(t *testing.T) {
	// Conformance 3: the current archive, unreachable, whose BackupFor names the
	// folder itself (no capture registered under another folder): BackupFor is
	// cleared with Out, so nothing re-adds the row (§5.2).
	cur := filepath.Join(t.TempDir(), "gone")
	r, x := unlistRig(t, cur)
	x.Deps.Settings = Settings{Current: cur, CaptureBackupFor: cur}
	r.ptr.p = Pointers{Out: cur, BackupFor: cur, LastArchive: cur}
	res := yes(t, cur, x)
	if !res.Removed || r.ptr.p != (Pointers{}) {
		t.Fatalf("%v pointers %+v", res.Lines, r.ptr.p)
	}
}

// covers: MA-969, R36
func TestRemoveFromListStopsOnlyWhatTheQuestionNamed(t *testing.T) {
	// Conformance 10: a schedule installed between the question and Yes was not
	// named, so Yes refuses and changes nothing; asked again, it is named and stopped.
	dir := smallArchive(t, t.TempDir())
	def := schedule.DefaultNameFor(dir)
	r, x := unlistRig(t, dir)
	r.sched.state[def] = schedule.NotInstalled
	q, err := PlanUnlist(dir, x)
	if err != nil || len(q.Named) != 0 {
		t.Fatalf("named %q %v", q.Named, err)
	}
	x.Named = q.Named
	r.sched.state[def] = schedule.Installed
	res := Unlist(dir, x)
	assure.Refused(t, res.ExitCode(), strings.Join(res.Lines, "\n"), assure.Code(1), assure.Names(def+" was not named", "Nothing was changed"),
		assure.NoSideEffect(func() bool { return len(r.sched.removed) == 0 && len(r.rows) == 0 && r.ptr.saves == 0 }))
	if res := yes(t, dir, x); !res.Removed || !contains(r.sched.removed, def) {
		t.Fatalf("asked again: %v stopped %q", res.Lines, r.sched.removed)
	}
}
