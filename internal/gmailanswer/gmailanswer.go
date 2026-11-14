// Package gmailanswer keeps the person's answer to the Gmail question (Q-B,
// docs/design-gmail-dedup.md §7.4 fold c) on this computer, for every archive, never in the
// archive (a field there would make it version 7, or be dropped by an older binary's save).
//
// Slice B3 kept the answer only in the archive's job file, so an archive with no backup settings
// on this computer could not be answered on its card, and a Not now typed at a terminal was not
// remembered at all (the B3 friction review's F2 and F7). The answer now lives:
//
//   - in the archive's job file when it has one (job.Job's GmailNotNow, GmailYes and
//     GmailArchive), when it is given in MailArchive Desktop;
//   - otherwise, and always for a Not now typed at a terminal, in a small file of this computer's,
//     <key>.gmail-answer beside the job files, where key is schedule.DefaultNameFor of the folder
//     as this computer's file system tells folders apart (folderKey: links resolved, and letter
//     case ignored on macOS and Windows; the B3 adversarial pass's A3). Not .json, so the job
//     folder's scan never reads one as a job; uninstall, Delete and Remove from list remove it.
//
// Every answer names the archive it was given for: the id the archive's last-run record carries
// (state.ArchiveID; the B3 adversarial pass's A1). Read returns an answer only while the archive
// in the folder carries that id, so a yes or a Not now given for one archive is never read for
// another archive that later sits in the same folder; that archive is asked again. A yes is
// spent once a backup has switched its stores on (the record's gmail_on): Read leaves those
// stores out, and Settle removes them.
//
// Read joins the job file's answer and the file's: Not now when either says so, and the yes for
// every store either names. A Desktop answer written into the job file removes the file, so the
// two never disagree.
package gmailanswer

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// Suffix ends the answer file's name: <key>.gmail-answer, beside the job files.
const Suffix = ".gmail-answer"

// TempPattern names the answer file's temporary copy while it is written: uninstall removes one a
// crash left behind.
const TempPattern = ".gmail-answer-*.tmp"

// version is the answer file's format.
const version = 1

const maxBytes = 16 * 1024

// ErrNoArchiveID refuses to keep an answer that names no archive: the archive has no record of a
// backup on this computer to bind it to.
var ErrNoArchiveID = errors.New("this archive has no record of a backup here to keep the answer for; back it up once, then answer")

// Answer is the person's answer kept on this computer for one archive.
type Answer struct {
	Version int `json:"version"`
	// Archive is the archive the answer was given for (state.LastRun.Archive).
	Archive string `json:"archive,omitempty"`
	// NotNow: the person said Not now; this computer does not ask again by itself.
	NotNow bool `json:"not_now,omitempty"`
	// Yes are the store folders (tokens) the person said yes for on the archive's card; the
	// archive's next backup switches them on.
	Yes []string `json:"yes,omitempty"`
}

// empty says the answer holds nothing to keep.
func (a Answer) empty() bool { return !a.NotNow && len(a.Yes) == 0 }

// folderKey is out as this computer's file system tells folders apart: absolute, its links
// resolved, and on macOS and Windows, whose file systems ignore letter case, in lower case. Two
// spellings of one folder give one key (A3). A case-sensitive volume on macOS may give two
// folders one key; the answer's archive id still keeps one archive's answer from the other.
func folderKey(out, goos string) string {
	abs := out
	if a, err := filepath.Abs(out); err == nil {
		abs = a
	}
	abs = util.ResolveExisting(abs)
	if goos == "darwin" || goos == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs
}

// Path is the answer file for the archive at out; "" with an error when this computer has no
// settings folder or out is empty.
func Path(out string) (string, error) {
	if strings.TrimSpace(out) == "" {
		return "", errors.New("no archive folder")
	}
	d, err := job.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, schedule.DefaultNameFor(folderKey(out, runtime.GOOS))+Suffix), nil
}

// jobFor is the archive's job file on this computer and its path; ok false when there is none,
// it cannot be read, or it is another folder's (a collision of the name's digest).
func jobFor(out string) (job.Job, string, bool) {
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		return job.Job{}, "", false
	}
	if _, err := os.Lstat(p); err != nil {
		return job.Job{}, p, false
	}
	j, err := job.Read(p)
	if err != nil || folderKey(j.Out, runtime.GOOS) != folderKey(out, runtime.GOOS) {
		return job.Job{}, p, false
	}
	return j, p, true
}

// readFile is the answer file at p: absent, unreadable or damaged is no answer.
func readFile(p string) Answer {
	data, err := util.ReadSecureFile(p, "Gmail answer", maxBytes)
	if err != nil {
		return Answer{}
	}
	var a Answer
	if json.Unmarshal(data, &a) != nil || a.Version > version {
		return Answer{}
	}
	return a
}

// spent is the stores the archive's last backup that ended ok found switched on: a yes for them
// has been applied.
func spent(lr state.LastRun) []string {
	if lr.Status != state.RunOK {
		return nil
	}
	return lr.GmailOn
}

// Read is the answer this computer keeps for the archive at out: the job file's and the answer
// file's together, each only when it was given for the archive the folder holds now, without the
// stores a backup has already switched on. Its Archive is that archive's id. No answer at all is
// an Answer with no NotNow and no Yes.
func Read(out string) Answer {
	lr, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent || lr.Archive == "" {
		return Answer{}
	}
	a := Answer{Archive: lr.Archive}
	done := spent(lr)
	take := func(notNow bool, yes []string) {
		a.NotNow = a.NotNow || notNow
		for _, t := range yes {
			if !slices.Contains(done, t) && !slices.Contains(a.Yes, t) {
				a.Yes = append(a.Yes, t)
			}
		}
	}
	if p, err := Path(out); err == nil {
		if f := readFile(p); f.Archive == lr.Archive {
			take(f.NotNow, f.Yes)
		}
	}
	if j, _, ok := jobFor(out); ok && j.GmailArchive == lr.Archive {
		take(j.GmailNotNow, j.GmailYes)
	}
	return a
}

// Keep keeps a, given in MailArchive Desktop, for the archive at out: in its job file when it has
// one that reads (the answer file, if any, is then removed), otherwise in the answer file. a must
// name the archive (Archive, from the record the question was read from). It never writes the
// archive.
func Keep(out string, a Answer) error {
	if !state.ValidArchiveID(a.Archive) {
		return ErrNoArchiveID
	}
	if j, p, ok := jobFor(out); ok {
		j.GmailNotNow, j.GmailYes, j.GmailArchive = a.NotNow, append([]string(nil), a.Yes...), a.Archive
		if a.empty() {
			j.GmailArchive = ""
		}
		if err := job.Write(p, j); err != nil {
			return err
		}
		return removeFile(out)
	}
	return KeepHere(out, a)
}

// KeepHere keeps a for the archive at out in the answer file only: a Not now typed at a terminal,
// which never writes a job file. a must name the archive. An answer with nothing in it removes
// the file.
func KeepHere(out string, a Answer) error {
	if !state.ValidArchiveID(a.Archive) {
		return ErrNoArchiveID
	}
	if a.empty() {
		return removeFile(out)
	}
	p, err := Path(out)
	if err != nil {
		return err
	}
	a.Version = version
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(p, data, TempPattern)
}

// removeFile removes the answer file for out; one already gone is no error.
func removeFile(out string) error {
	fp, err := Path(out)
	if err != nil {
		return nil
	}
	if err := os.Remove(fp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Settle clears what a backup of the archive at out has spent: once its last backup ended ok,
// the stores it found switched on are taken out of the yes kept for that archive (the job file's
// and the file's), and an answer left with nothing in it is removed. An answer for another
// archive, or a Not now, is left as it is. Best effort: Read leaves spent stores out anyway.
// Every caller that runs a backup with a kept yes calls it after the run.
func Settle(out string) error {
	lr, st, _ := state.ReadLastRun(out)
	done := spent(lr)
	if st != state.LastRunPresent || lr.Archive == "" || len(done) == 0 {
		return nil
	}
	drop := func(yes []string) ([]string, bool) {
		keep := yes[:0:0]
		for _, t := range yes {
			if !slices.Contains(done, t) {
				keep = append(keep, t)
			}
		}
		return keep, len(keep) != len(yes)
	}
	var firstErr error
	if p, err := Path(out); err == nil {
		if f := readFile(p); f.Archive == lr.Archive {
			if yes, changed := drop(f.Yes); changed {
				f.Yes = yes
				if err := KeepHere(out, f); err != nil {
					firstErr = err
				}
			}
		}
	}
	if j, p, ok := jobFor(out); ok && j.GmailArchive == lr.Archive {
		if yes, changed := drop(j.GmailYes); changed {
			j.GmailYes = yes
			if !j.GmailNotNow && len(yes) == 0 {
				j.GmailArchive = ""
			}
			if err := job.Write(p, j); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// CarryInto keeps the answer of the job file j replaces: a job file written afresh for the
// archive at out (the wizard's schedule offer, and so Add another archive) carries the Gmail
// answer the job file there now holds, so a rewrite never forgets it (the B3 adversarial pass's
// A7).
func CarryInto(out string, j *job.Job) {
	if cur, _, ok := jobFor(out); ok && cur.GmailArchive != "" {
		j.GmailNotNow, j.GmailYes, j.GmailArchive = cur.GmailNotNow, append([]string(nil), cur.GmailYes...), cur.GmailArchive
	}
}
