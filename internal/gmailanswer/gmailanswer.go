// Package gmailanswer keeps the person's answer to the Gmail question (Q-B,
// docs/design-gmail-dedup.md §7.4 fold c) on this computer, for every archive, never in the
// archive (a field there would make it version 7, or be dropped by an older binary's save).
//
// Slice B3 kept the answer only in the archive's job file, so an archive with no backup settings
// on this computer could not be answered on its card, and a Not now typed at a terminal was not
// remembered at all (the B3 friction review's F2 and F7). The answer now lives:
//
//   - in the archive's job file when it has one (job.Job's GmailNotNow and GmailYes, as B3 kept
//     it), when it is given in MailArchive Desktop;
//   - otherwise, and always for a Not now typed at a terminal, in a small file of this computer's,
//     <key>.gmail-answer beside the job files, where key is schedule.DefaultNameFor(out), the
//     name every per-archive file of this computer uses. Not .json, so the job folder's scan never
//     reads one as a job; uninstall removes them by this suffix.
//
// Read joins the two: Not now when either says so, and the yes for every store either names. A
// Desktop answer written into the job file removes the file, so the two never disagree.
package gmailanswer

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// Suffix ends the answer file's name: <key>.gmail-answer, beside the job files.
const Suffix = ".gmail-answer"

// version is the answer file's format.
const version = 1

const maxBytes = 16 * 1024

// Answer is the person's answer kept on this computer for one archive.
type Answer struct {
	Version int `json:"version"`
	// NotNow: the person said Not now; this computer does not ask again by itself.
	NotNow bool `json:"not_now,omitempty"`
	// Yes are the store folders (tokens) the person said yes for on the archive's card; the
	// archive's next backup switches them on.
	Yes []string `json:"yes,omitempty"`
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
	return filepath.Join(d, schedule.DefaultNameFor(out)+Suffix), nil
}

// jobFor is the archive's job file on this computer and its path; ok false when there is none or
// it cannot be read.
func jobFor(out string) (job.Job, string, bool) {
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		return job.Job{}, "", false
	}
	if _, err := os.Lstat(p); err != nil {
		return job.Job{}, p, false
	}
	j, err := job.Read(p)
	return j, p, err == nil
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

// Read is the answer this computer keeps for the archive at out: the job file's and the answer
// file's together. No answer at all is the zero Answer.
func Read(out string) Answer {
	var a Answer
	if p, err := Path(out); err == nil {
		a = readFile(p)
	}
	if j, _, ok := jobFor(out); ok {
		a.NotNow = a.NotNow || j.GmailNotNow
		for _, t := range j.GmailYes {
			a.Yes = appendOnce(a.Yes, t)
		}
	}
	a.Version = 0
	return a
}

// Keep keeps a, given in MailArchive Desktop, for the archive at out: in its job file when it has
// one that reads (the answer file, if any, is then removed), otherwise in the answer file. It
// never writes the archive.
func Keep(out string, a Answer) error {
	if j, p, ok := jobFor(out); ok {
		j.GmailNotNow, j.GmailYes = a.NotNow, append([]string(nil), a.Yes...)
		if err := job.Write(p, j); err != nil {
			return err
		}
		if fp, err := Path(out); err == nil {
			if err := os.Remove(fp); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		return nil
	}
	return KeepHere(out, a)
}

// KeepHere keeps a for the archive at out in the answer file only: a Not now typed at a terminal,
// which never writes a job file.
func KeepHere(out string, a Answer) error {
	p, err := Path(out)
	if err != nil {
		return err
	}
	a.Version = version
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(p, data, ".gmail-answer-*.tmp")
}

func appendOnce(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}
