package tbrestore

import (
	"errors"
	"runtime"
	"strings"
)

// Refusal is a restore that does not go ahead, or a step that cannot be taken, in
// the words the person sees (design §7.5: what happened first, then the remedy).
// Code is the words' id ("W-RUNNING"); Err, when set, is the cause underneath, kept
// for errors.Is and never shown.
type Refusal struct {
	Code  string
	Words string
	Err   error
}

func (r *Refusal) Error() string { return r.Words }
func (r *Refusal) Unwrap() error { return r.Err }

// RefusalCode is err's words id when err is a *Refusal, else "".
func RefusalCode(err error) string {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

func refuse(code, words string, cause error) error {
	return &Refusal{Code: code, Words: words, Err: cause}
}

// fill replaces each placeholder in capitals ("PATH") with its value.
func fill(words string, kv ...string) string {
	r := strings.NewReplacer(kv...)
	return r.Replace(words)
}

// The words of the Thunderbird side (design §7.5 and its folded rows), as the slice
// that refuses with each builds it. Placeholders in capitals are filled by the caller.
const (
	// W-RUNNING; on Windows wRunningWindows is added.
	wRunning        = "Thunderbird is open, so nothing was restored. Close Thunderbird (on Windows: File → Exit; on a Mac: Thunderbird → Quit Thunderbird), then click Check again."
	wRunningWindows = " If you don't see Thunderbird, look for its icon by the clock; someone else signed in to this computer may also have it open."

	wRestoring = "Another restore into this Thunderbird profile is running (in MailArchive or at a terminal), so this one did not start. Wait for it to finish, then click Check again."

	wNoProf   = "No Thunderbird profile was found on this computer, so there is nowhere to restore to. Open Thunderbird once to make one, close it, then click Restore to Thunderbird again."
	wProfINI  = "Thunderbird's list of profiles (profiles.ini) is larger than MailArchive will read, so nothing was restored. Remove profiles you no longer use in Thunderbird's Profile Manager, then try again."
	wProfMode = "This profile's folder (PATH) can be changed by other accounts on this computer, or belongs to another account, so MailArchive will not write into it. Make it yours only (for example `chmod go-w PATH`), or choose another profile."
	// wProfChanged: the profile list a position was chosen from is not the list there now
	// (design §3.1 step 6 note).
	wProfChanged = "The list of Thunderbird profiles changed. Choose the profile again."
	wNotProfile  = "PATH is not a Thunderbird profile (it holds no prefs.js), so nothing was restored. Choose a Thunderbird profile folder."

	wPrefs = "MailArchive could not read this profile's settings file (FILE: WHY), so nothing was restored. Open Thunderbird once with this profile and close it, then try again; if it repeats, choose another profile."
	// W-NOLOCAL, as replaced by lab finding 1. MENU PATH is lab row MA-1133's, and these
	// words do not ship before it has run (design §3.2 step 1 note); until then the
	// placeholder stays in the text so no slice can ship a guessed path.
	wNoLocal   = "This Thunderbird profile has no Local Folders yet, so there is nowhere to restore to and nothing was written. In Thunderbird, MENU PATH, then close Thunderbird and click Check again. MailArchive does not change Thunderbird's settings itself."
	wMaildir   = "This Thunderbird profile keeps each message in its own file (the 'maildir' setting). MailArchive can restore only into the usual one-file-per-folder kind, so nothing was written. Choose another profile."
	wOverlap   = "Thunderbird's Local Folders (PATH) overlaps WHAT, so nothing was restored. Choose another profile."
	wNoIdent   = "Windows could not identify the drive that holds Thunderbird's Local Folders (PATH), so nothing was restored. This happens on some network drives. Choose a profile on this computer's own drive."
	wNetwork   = "Thunderbird's Local Folders (PATH) is on a network drive. The restore writes there over the network, so it may be slow."
	wStore     = "If you use Thunderbird from the Microsoft Store, this restore may not show in it: that version may keep its mail in another place. If you installed Thunderbird from thunderbird.net, ignore this."
	wOldTB     = "This profile was last opened by Thunderbird VERSION. That version shows a '>' at the start of any message line beginning with 'From '. Update Thunderbird first to see those lines exactly, or go ahead."
	wLocalGone = "Thunderbird's Local Folders for this profile (PATH) is not there or is not a folder, so nothing was restored. Open Thunderbird once with this profile and close it, then click Check again."
)

// RestoringRefusal is W-RESTORING, for the plan that finds the restore lock held
// (RestoreLockHeld) before it takes anything (slice R6's engine).
func RestoringRefusal() error { return refuse("W-RESTORING", wRestoring, nil) }

// NoProfileRefusal is W-NOPROF: no Thunderbird profile on this computer.
func NoProfileRefusal() error { return refuse("W-NOPROF", wNoProf, nil) }

// RunningWords is W-RUNNING as this platform shows it.
func RunningWords() string { return runningWords(runtime.GOOS) }

func runningWords(goos string) string {
	if goos == "windows" {
		return wRunning + wRunningWindows
	}
	return wRunning
}

// WaitWords is W-WAIT, shown for the whole run in Desktop and at a terminal (design
// §5.2 note item 5, lab finding 3). Windows names the Cancel of Thunderbird's own
// dialog (lab B saw "Close Thunderbird" and "Cancel"); macOS and Linux get the lead's
// platform-neutral words (2026-11-07, owner-delegated 2026-11-02), because the macOS
// dialog's words and buttons are not yet seen (lab row MA-1134) and Linux showed only
// a message.
func WaitWords() string { return waitWords(runtime.GOOS) }

func waitWords(goos string) string {
	const lead = "While this runs, leave Thunderbird closed. "
	if goos == "windows" {
		return lead + "If you open it and it says Thunderbird is already running, choose Cancel and wait for the restore to finish."
	}
	return lead + "If Thunderbird says it is already open or running, close that message and wait for the restore to finish."
}
