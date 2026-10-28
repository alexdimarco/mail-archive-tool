package vault

import (
	"errors"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// covers: MA-854, R37
// A restore whose folder's identity pin did not take is not prepared (the P3-8 fixes'
// pass, WINPATH-1): on Windows the folder's identity would be read only at Commit,
// from whatever folder is at the path then, so a folder swapped in after a failed pin
// would pass the check and receive the key file. A pin fails for real only in a race
// on Windows, so it is injected. Positive twin: the pin takes and the restore is
// prepared. Nothing is written either way.
func TestARestoreWhosePinDidNotTakeIsNotPrepared(t *testing.T) {
	lowCost(t)
	out, _, rk, file := sealedArchive(t)
	dropKeyFiles(t, out)
	p, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
	if err != nil {
		t.Fatalf("positive twin: the restore is prepared: %v", err)
	}
	p.Discard()

	restore := util.FailPinsForTest()
	defer restore()
	p, err = PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
	code, msg := 0, ""
	if err != nil {
		msg = err.Error()
		if errors.Is(err, ErrConflict) && p == nil {
			code = 1
		}
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "nothing was written", "try again"),
		assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, FileName)) && !exists(filepath.Join(out, BackupName)) }))
}
