package keyring

import (
	"errors"
	"fmt"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/encrecord"
)

// Apply makes opts a keyed run of u's archive (§3.1): the key and the id this
// computer recorded for the folder, which the run's preflight checks the key
// against (§6). Only an open for a write records the folder, so only one may run
// a backup. The key stays in use until done is called; a Lock meanwhile takes
// effect when it is.
func Apply(opts *app.Options, u *Unlocked) (done func(), err error) {
	if !u.write {
		return nil, errors.New("this archive was opened to read, not to back up into; open it for a backup")
	}
	if encrecord.Spelling(opts.Out) != encrecord.Spelling(u.out) {
		return nil, fmt.Errorf("the archive opened is %s, not %s", u.out, opts.Out)
	}
	k, _, err := u.acquire()
	if err != nil {
		return nil, err
	}
	opts.Key = k
	opts.RecordedID = u.id
	return u.release, nil
}

// OutlookPSTDir is where -outlook's PSTs for u's archive are exported: MailArchive's
// working folder for the archive, never the archive itself, removed by cleanup on
// every exit (phase3 §6, E2).
func OutlookPSTDir(u *Unlocked) (dir string, cleanup func(), err error) {
	k, _, err := u.acquire()
	if err != nil {
		return "", nil, err
	}
	defer u.release()
	return app.OutlookPSTDir(u.out, k)
}
