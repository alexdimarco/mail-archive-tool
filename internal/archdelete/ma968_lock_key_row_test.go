package archdelete

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/lockfile"
)

// covers: MA-968, R36
func TestTheLockFileGoesAfterUnlockingAndThenTheFolder(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	writeFile(t, filepath.Join(dir, ".DS_Store"), "litter")
	r := newRig(t, dir)
	var seen []string
	faultOn(t, &seen, func(rel string) error {
		switch {
		// On macOS and Linux the delete holds the released lock again for the instant
		// of removing its file (removeLockFile), which it could not take had the
		// delete not released it; Windows removes it unheld.
		case rel == lockName && runtime.GOOS == "windows" && lockfile.Held(filepath.Join(dir, lockName)):
			t.Error("the lock file is removed only after the lock is released")
		case rel != lockName && !lockfile.Held(filepath.Join(dir, lockName)):
			t.Errorf("%s was removed without the archive lock", rel)
		}
		return nil
	})
	res := r.run(t, false)
	if res.Status != StatusComplete || exists(dir) || !res.FolderGone {
		t.Fatalf("%s", res.Message())
	}
	if !contains(seen, lockName) {
		t.Fatalf("removed: %q", seen)
	}
	// A lock file that cannot be removed: the delete is still complete, and says so.
	dir = smallArchive(t, t.TempDir())
	r = newRig(t, dir)
	faultOn(t, nil, func(rel string) error {
		if rel == lockName {
			return inUseErr(rel)
		}
		return nil
	})
	res = r.run(t, false)
	if res.Status != StatusComplete || !res.LockFileLeft || !hasLine(res, msgLockFileLeft) || !exists(filepath.Join(dir, lockName)) {
		t.Fatalf("%s", res.Message())
	}
	if hasLine(res, "The folder is kept: it holds") {
		t.Fatal("the lock file is never counted as the person's file")
	}
	// Amended 2026-11-03 (design-archive-delete §15.2, DM-27, MA-1046): the `marked` line before step
	// 6's first removal.
	if ev := events(logLines(t, r.x.Log)); strings.Join(ev, ",") != "started,marked,folder kept,finished complete" {
		t.Fatalf("log %q", ev)
	}
}

// covers: MA-968, R36
func TestAnEncryptedArchiveGoneForgetsItsRecordAndKeyThenItsRow(t *testing.T) {
	dir := encryptedClean(t)
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	res := r.run(t, false)
	if res.Status != StatusComplete || exists(dir) || res.KeyForgotten != "forgotten" || !hasLine(res, "This computer forgot the archive's key") {
		t.Fatalf("%s", res.Message())
	}
	if len(r.kr.asked) != 1 || r.kr.asked[0] != dir || len(r.kr.forgot) != 1 || !r.kr.forgot[0] || len(r.rows) != 1 {
		t.Fatalf("asked %q forgot %v rows %q", r.kr.asked, r.kr.forgot, r.rows)
	}
	if !strings.Contains(string(res.Record), "This computer's record and key: forgotten\n") {
		t.Fatalf("record:\n%s", res.Record)
	}

	// Another recorded copy keeps the key: only this path is forgotten.
	dir = encryptedClean(t)
	r = newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	r.kr.plan = KeyPlan{ID: testArchiveID, Gone: true, Last: false, Others: []string{`E:\Backups\Vault`}}
	res = r.run(t, false)
	if res.Status != StatusComplete || len(r.kr.forgot) != 1 || r.kr.forgot[0] || !hasLine(res, `E:\Backups\Vault`) || len(r.rows) != 1 {
		t.Fatalf("%s (forgot %v)", res.Message(), r.kr.forgot)
	}
}

// covers: MA-968, R36
func TestAFolderKeptOnlyForTheLockFileIsRemovedByStep8sRetry(t *testing.T) {
	dir := encryptedClean(t)
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	held := true
	r.kr.onForget = func() { held = false }
	faultOn(t, nil, func(rel string) error {
		if held && rel == lockName {
			return inUseErr(rel)
		}
		return nil
	})
	res := r.run(t, false)
	if res.Status != StatusComplete || exists(dir) || !res.FolderGone || !hasLine(res, "The folder is gone.") {
		t.Fatalf("%s", res.Message())
	}
	if hasLine(res, msgLockFileLeft) || hasLine(res, msgEmptyFolderLeft) {
		t.Fatalf("step 7's lock-file line is dropped once step 8 removed the folder:\n%s", res.Message())
	}
	// Amended 2026-11-03 (design-archive-delete §15.2, DM-27, MA-1046): the `marked` line before step
	// 6's first removal.
	if ev := strings.Join(events(logLines(t, r.x.Log)), ","); ev != "started,marked,folder kept,key forgotten,folder removed,finished complete" {
		t.Fatalf("log %s", ev)
	}

	// A kept folder that still holds the archive's leftovers: step 8 does not apply
	// yet, it is skipped and the row kept with its Forget button.
	dir = encryptedArchive(t) // units/ab/stray.txt and inventory/notes.txt are kept
	r = newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	res = r.run(t, false)
	if !res.KeySkipped || !hasLine(res, msgKeySkipped) || len(r.kr.asked) != 0 || len(r.rows) != 0 {
		t.Fatalf("%s", res.Message())
	}
	if exists(filepath.Join(dir, lockName)) {
		t.Fatal("no .mailarchive.lock is left in the kept folder")
	}
}

// covers: MA-968, R36
func TestStep8SkippedWhenARunHoldsTheLockAndStep9AndStep10SayWhatFailed(t *testing.T) {
	dir := encryptedClean(t)
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	r.kr.forgetErr = fmt.Errorf("in use: %w", lockfile.ErrHeld)
	res := r.run(t, false)
	if !res.KeySkipped || !hasLine(res, msgKeySkipped) || len(r.rows) != 0 || res.KeyForgotten != "" {
		t.Fatalf("%s", res.Message())
	}

	// Step 9: the row cannot be removed.
	r = newRig(t, smallArchive(t, t.TempDir()))
	r.rowErr = errors.New("known-archives.json is read-only")
	res = r.run(t, false)
	if !hasLine(res, msgRowFailed) || hasLine(res, "Removed from Your archives") || res.RowRemoved {
		t.Fatalf("%s", res.Message())
	}

	// Step 10: the finished line cannot be written.
	r = newRig(t, smallArchive(t, t.TempDir()))
	r.sched.onRemove = func(string) {
		if runtime.GOOS == "windows" {
			os.Chmod(r.x.Log, 0o444)
		} else if err := os.Chmod(r.x.Log, 0o400); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { os.Chmod(r.x.Log, 0o600) })
	if os.Getuid() == 0 {
		t.Skip("root writes a read-only file")
	}
	res = r.run(t, false)
	if !hasLine(res, msgDigestFailed) || res.Record == nil {
		t.Fatalf("%s", res.Message())
	}
}
