package schedule

import (
	"errors"
	"path/filepath"
)

// ErrUnsafeLocation is returned by ProgramForSchedule when the program's install
// directory (or an ancestor) can be written by an account other than the owner,
// so another account could replace the scheduled binary and the next unattended
// run would execute their code as the owner — the Q20 threat (§5.3, owner ruling
// 13.44: refuse, do NOT accept as a residual). It carries no path; its message is
// the relocation/reinstallation remedy every scheduling caller shows.
var ErrUnsafeLocation = errors.New("Mail Archive is installed in a folder another account on this computer can change, so a scheduled run could be made to run someone else's program as you. Do this: move Mail Archive into a folder only you or an administrator can write — on Windows, Program Files or your own user folder; on macOS or Linux, your Applications or home folder, not a world-writable or shared one — then install the schedule (or choose Repair) again.")

// locationWritableByOthers reports whether the program directory dir can be
// written by an account other than the owner (Q20's permissions policy). A
// package var so the pure core stays testable and the real per-OS mechanism is
// swapped in: on Unix a world-writable mode walk (location_other.go, enforced),
// on Windows a DACL comparison (location_windows.go, MA-649) that stays dormant
// until lab item L-W15 verifies the read on a real box — a DACL walk written
// blind on another OS is untrustworthy security code, so it is gated, not shipped
// active (the l2WrapperGateHolds pattern).
var locationWritableByOthers = locationWritableByOthersOS

// guardLocation applies Q20 to a resolved program path: it returns
// ErrUnsafeLocation when the program's directory is writable by another account,
// else nil. ProgramForSchedule calls it after choosing the program, so every
// scheduling caller (runSchedule, offerSchedule, repair, backupSpecFor, the
// card's run-job repair) refuses from one chokepoint.
func guardLocation(exe string) error {
	if locationWritableByOthers(filepath.Dir(exe)) {
		return ErrUnsafeLocation
	}
	return nil
}
