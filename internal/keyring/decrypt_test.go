package keyring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// The unencrypted copy of a real encrypted archive, through its key (phase 3 §10,
// P3-4; MA-821).

// copyPlan plans a copy of out into a fresh folder.
func copyPlan(t *testing.T, out string) (*app.CopyExportEnv, *app.DecryptPlan) {
	t.Helper()
	e := app.NewCopyExportEnv("", "", "test")
	p, err := e.PlanDecryptCopy(out, t.TempDir(), nil, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

// covers: MA-821, R37
// A copy made through the key is a plaintext archive a plaintext verify attests
// whole — every message's files, their recorded checksums kept — with search, pages
// and history; the encrypted archive is unchanged, and the key the copy was given
// still opens it afterwards (the copy used a lease of its own and destroyed it).
func TestACopyThroughTheKeyIsAWholePlaintextArchive(t *testing.T) {
	out, u := fullFixture(t)
	before := treeOf(t, out)
	e, p := copyPlan(t, out)
	rep, err := DecryptCopy(context.Background(), u, e, p, nil, nil, quiet, nil)
	if err != nil || rep.ExitCode() != 0 || rep.Copied == 0 || rep.Copied != rep.Messages || rep.Index != "copied" {
		t.Fatalf("the copy: %+v %v", rep, err)
	}
	v, err := app.Verify(p.Dest, app.VerifyOptions{}, quiet, nil)
	if err != nil || !v.Attested() {
		t.Fatalf("a plaintext verify of the copy: %+v %v\n%s", v, err, strings.Join(app.VerifySummary(v), "\n"))
	}
	for _, n := range []string{"search.db", state.HistoryName, "README.txt", "index.html"} {
		if _, err := os.Stat(filepath.Join(p.Dest, n)); err != nil {
			t.Fatalf("the copy lacks %s", n)
		}
	}
	after := treeOf(t, out)
	delete(after, lockfile.Name)
	delete(before, lockfile.Name)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("the copy wrote into the encrypted archive")
	}
	l, err := u.Lease()
	assure.Reached(t, err == nil && l != nil, "the key the copy was given still works")
	l.Destroy()
}

// covers: MA-821, R37
// An archive locked before the copy starts is refused as locked — never damage — with
// nothing made (§3.2, the CRIT-03 lease).
func TestACopyOfALockedArchiveIsRefusedAsLocked(t *testing.T) {
	out, u := fullFixture(t)
	e, p := copyPlan(t, out)
	u.Lock()
	_, err := DecryptCopy(context.Background(), u, e, p, nil, nil, quiet, nil)
	_, statErr := os.Stat(p.Dest)
	assure.Refused(t, 1, errText(err), assure.Code(1), assure.Names("locked"),
		assure.NoSideEffect(func() bool { return errors.Is(err, app.ErrCopyLocked) && os.IsNotExist(statErr) }))
}

// covers: MA-821, R37
// A sealed message file that is damaged leaves that message out — listed inside the
// copy, the rest copied, exit 3 — never a garbled file in the copy.
func TestACopyLeavesADamagedMessageOutAndSaysSo(t *testing.T) {
	out, u := fullFixture(t)
	unit := aUnitFile(t, out)
	b, _ := os.ReadFile(unit)
	b[len(b)-1] ^= 1
	os.WriteFile(unit, b, 0o600)
	e, p := copyPlan(t, out)
	rep, err := DecryptCopy(context.Background(), u, e, p, nil, nil, quiet, nil)
	if err != nil || rep.ExitCode() != 3 || rep.Skipped != 1 || rep.Copied != rep.Messages-1 {
		t.Fatalf("a damaged unit: %+v %v", rep, err)
	}
	problems, _ := os.ReadFile(filepath.Join(p.Dest, app.CopyProblemsName))
	assure.Reached(t, strings.Contains(string(problems), "damaged"), "the damaged message is listed inside the copy")
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
