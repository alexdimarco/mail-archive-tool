package archdelete

import (
	"time"

	"mail-archive-tool/internal/schedule"
)

// The seams Execute and Unlist act through (docs/design-archive-delete.md §5.2,
// §5.3, slice 1b). Each is small, so the order of the steps, their success rules
// and their words live here, in one place, and the Desktop and the command line
// (slices 3 and 4) only say how each step reaches the scheduler, the settings and
// the keys of this computer.

// Scheduler is the host scheduler, by entry name (§5.3 step 2). The real one asks
// schedule.Query, schtasks and the side-files beside schedule.DefaultWrapperPath,
// each call with a time limit (§5.1 "Each scheduler call has a time limit").
type Scheduler interface {
	// Query is what the scheduler says about the entry (schedule.Query).
	Query(name string) schedule.State
	// SaysMissing reports, on Windows, that `schtasks /Query /TN name` failed saying
	// the task does not exist (schedule's schtasksSaysMissing). Its wording is the
	// system's own: on a Windows in another language it is false.
	SaysMissing(name string) bool
	// SideFilesExist reports, on Windows, that the entry's <name>.xml or <name>.cmd
	// is still beside the wrapper folder (schedule.DefaultWrapperPath).
	SideFilesExist(name string) bool
	// RemoveSideFiles removes those side-files, either already gone being no error
	// (schedule's removeSchtasksSideFiles).
	RemoveSideFiles(name string) error
	// RemoveLiveEntry removes the entry from the host scheduler without touching the
	// archive (schedule.RemoveLiveEntry).
	RemoveLiveEntry(name string) error
}

// Pointers are the settings that point at an archive (§5.3 step 4, §5.2): the
// Desktop's current archive (Out) and the folder its Microsoft 365 capture is
// registered under (BackupFor), and last-archive.txt.
type Pointers struct {
	Out         string
	BackupFor   string
	LastArchive string
}

// PointerStore reads and writes Pointers. Save writes the settings once: a failed
// Save leaves them as they were.
type PointerStore interface {
	Load() (Pointers, error)
	Save(Pointers) error
}

// KeyRegistry is the Desktop's registry of open encrypted archives (§5.3 step 1
// (b), the Desktop implements it in slice 3): LockForDelete, all under the
// registry's lock, does what Keys.lock does for this archive (removes its entry,
// sets held, bumps gen), sets the archive's "being deleted" mark that every open
// checks — the person's own Unlock included, refused with "This archive is being
// deleted." — and returns only when the archive's reader is closed (e.close(true)).
// EndDelete clears the mark; Execute always calls it after LockForDelete, also
// after a refusal or a recovered panic.
type KeyRegistry interface {
	LockForDelete(folder string) error
	EndDelete(folder string)
}

// KeyPlan is the keyring's plan to forget an encrypted archive's record at one
// spelling (keyring.PlanForget).
type KeyPlan struct {
	// ID is the archive id of the record PlanForget found at the spelling
	// (keyring.ForgetPlan.ID): step 8 forgets it only when it is the ID the plan, or
	// the original started line, recorded (del-s1b-A1-05).
	ID string
	// Gone: the folder is gone, or encmark.At classifies it Gone.
	Gone bool
	// Last: this spelling is the archive's last recorded copy; Others are the rest.
	Last   bool
	Others []string
	// Opaque is the real plan, handed back to Forget.
	Opaque any
}

// Keyring forgets this computer's record of an encrypted archive and its remembered
// key (§5.3 step 8): PlanForget on the recorded Spelling, then Forget with Whole
// and Keys only when the plan is the last copy. Forget takes the archive's lock
// when the folder is there (keyring.Forget's lockFolder), which re-creates
// .mailarchive.lock; a lock a run holds fails it (lockfile.ErrHeld).
type Keyring interface {
	PlanForget(spelling string) (KeyPlan, error)
	Forget(p KeyPlan, whole bool) error
}

// ExecDeps are Execute's seams.
type ExecDeps struct {
	// Deps are Plan's: Execute plans the folder again under the archive lock and
	// compares (§3.1). Deps.Busy is asked again there, under the delete's own lock,
	// so it must not count that lock (the Desktop's own record of what runs, not
	// lockfile.Held).
	Deps Deps
	// Log is deletions.log's path; "" is DefaultLogPath.
	Log string
	// Row is the row's own spelling in Your archives (archindex), which step 9
	// removes; "" is the plan's folder.
	Row string
	// Scheduler stops the plan's entries (step 2). Required when the plan lists any.
	Scheduler Scheduler
	// Pointers clears Out, BackupFor and last-archive.txt (step 4). Required.
	Pointers PointerStore
	// Keys, ScratchDir and Keyring are an encrypted archive's (steps 1 (b), 1 (c) and
	// 5, 8). Required for an encrypted plan. ScratchDir is the archive's scratch
	// folder for one spelling of its folder and its archive id
	// (app.archiveScratch's rule).
	Keys       KeyRegistry
	ScratchDir func(spelling, archiveID string) (string, error)
	Keyring    Keyring
	// SettingsDir is the folder the Desktop keeps its settings, its list of archives
	// (known-archives.json) and the alert files in: filepath.Dir of its -config file;
	// "" is MailArchive's settings folder (job.Dir()). Step 3 removes the alert files
	// there as well as in job.Dir() (del-s1b-A3-04).
	SettingsDir string
	// ForgetRow removes the row (step 9); nil removes every entry naming the folder
	// from the list in SettingsDir. ErrRowNotListed means none named it.
	ForgetRow func(row string) error
	// BeforeStep, when set, is asked right before steps 4, 8 and 9: the command
	// line's re-check that no MailArchive Desktop is running (§3.3 [R3:C17]). An
	// error stops the delete there as not fully deleted, naming the steps done, and
	// the row shows "Delete not finished" (§5.4).
	BeforeStep func(step int) error
	// Now is the clock; nil is time.Now. Computer, Account and Version head the
	// deletion record (§8.1); "" Computer and Account are this computer's.
	Now      func() time.Time
	Computer string
	Account  string
	Version  string
}
