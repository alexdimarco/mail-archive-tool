package archdelete

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// The real scheduler seams of a delete (docs/design-archive-delete.md §5.1, §5.3 step 2;
// MA-973), shared by the Desktop and the command line: this computer's scheduler, each
// call with a time limit (§5.1 "Each scheduler call has a time limit").

// DefaultSchedulerTimeout is how long one scheduler call may take before it counts as not
// answered.
const DefaultSchedulerTimeout = 30 * time.Second

// errSchedulerSilent is a scheduler call that did not answer in time.
var errSchedulerSilent = errors.New("the computer's scheduler did not answer in time")

// timed runs fn with a time limit; ok is false when it did not return in time (it is left
// to finish on its own: a hung scheduler is not waited on).
func timed[T any](limit time.Duration, fn func() T) (v T, ok bool) {
	if limit <= 0 {
		limit = DefaultSchedulerTimeout
	}
	ch := make(chan T, 1)
	go func() { ch <- fn() }()
	select {
	case v = <-ch:
		return v, true
	case <-time.After(limit):
		return v, false
	}
}

// HostScheduler is this computer's scheduler as Execute and Unlist reach it (Scheduler):
// schedule.Query, schtasks's own "missing" words and the side-files on Windows, and
// schedule.RemoveLiveEntry, which never touches the archive. A call that does not answer
// within Timeout ("" is DefaultSchedulerTimeout) is never taken for an absent entry: a
// query is SchedulerUnavailable, a removal an error.
type HostScheduler struct {
	Timeout time.Duration
}

func (h HostScheduler) Query(name string) schedule.State {
	st, ok := timed(h.Timeout, func() schedule.State { return schedule.Query(name) })
	if !ok {
		return schedule.SchedulerUnavailable
	}
	return st
}

func (h HostScheduler) SaysMissing(name string) bool {
	v, ok := timed(h.Timeout, func() bool { return schedule.SchtasksSaysMissing(name) })
	return ok && v
}

func (h HostScheduler) SideFilesExist(name string) bool { return schedule.SideFilesExist(name) }

func (h HostScheduler) RemoveSideFiles(name string) error { return schedule.RemoveSideFiles(name) }

func (h HostScheduler) RemoveLiveEntry(name string) error {
	err, ok := timed(h.Timeout, func() error { return schedule.RemoveLiveEntryNamed(name) })
	if !ok {
		return errSchedulerSilent
	}
	return err
}

// derivedEntryRe is schedule.DefaultNameFor's shape, with or without -verify.
var derivedEntryRe = regexp.MustCompile(`^mailarchive-[0-9a-f]{8}(-verify)?$`)

// HostDiscovery is §5.1's discovery with the scheduler asked too: Files (FileDiscovery: the
// derived names, the folder's descriptors, the job files), then every entry this tool
// installed on this computer (schedule.OurInstalled, with a time limit) whose -out, read
// back where the host keeps it (schedule.InstalledOut: the Windows side-files, the Linux
// crontab block), is the same folder (schedule.SameArchivePath). macOS cannot read an entry
// back (UJ-6), so there every custom-named entry no job file binds is listed as "not
// removed: check it yourself". A scheduler that does not answer in time is said the same
// way, never taken for "no entries".
type HostDiscovery struct {
	Files FileDiscovery
	// Installed lists the entries (nil: schedule.OurInstalled); Out reads one back (nil:
	// schedule.InstalledOut). Seams for a test.
	Installed func() []schedule.InstalledEntry
	Out       func(name string) (string, bool)
	Timeout   time.Duration
}

// SchedulerSilentNote is listed among the "check it yourself" schedules when the scheduler
// did not answer while its entries were listed.
const SchedulerSilentNote = "this computer's scheduler did not answer when MailArchive listed its entries: check this folder's scheduled backups yourself"

// Discover implements Discoverer.
func (h HostDiscovery) Discover(folder string) (Discovery, error) {
	d, err := h.Files.Discover(folder)
	if err != nil {
		return d, err
	}
	goos := h.Files.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	list := h.Installed
	if list == nil {
		list = schedule.OurInstalled
	}
	readOut := h.Out
	if readOut == nil {
		readOut = schedule.InstalledOut
	}
	entries, ok := timed(h.Timeout, list)
	if !ok {
		d.CheckYourself = append(d.CheckYourself, SchedulerSilentNote)
		return d, nil
	}
	have := map[string]bool{}
	for _, e := range d.Entries {
		have[e.Name] = true
	}
	jobDir, _ := job.Dir()
	for _, ie := range entries {
		if ie.Name == "" || have[ie.Name] {
			continue
		}
		out, known := ie.Out, ie.Out != ""
		if !known {
			type res struct {
				out   string
				known bool
			}
			r, ok := timed(h.Timeout, func() res { o, k := readOut(ie.Name); return res{o, k} })
			out, known = r.out, ok && r.known
		}
		switch {
		case known:
			if schedule.SameArchivePath(out, folder, goos) {
				have[ie.Name] = true
				d.Entries = append(d.Entries, Entry{Name: ie.Name, From: "scheduler", Out: out})
				d.Spellings = appendSpelling(d.Spellings, out)
			}
		case goos == "darwin" && !derivedEntryRe.MatchString(ie.Name) && !jobFileNamed(jobDir, ie.Name):
			d.CheckYourself = append(d.CheckYourself, fmt.Sprintf("%s (macOS cannot say which folder it backs up)", ie.Name))
		}
	}
	return d, nil
}

// jobFileNamed reports a job file <name>.json in dir: the entry backs up the folder its Out
// names, which FileDiscovery already compared.
func jobFileNamed(dir, name string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Lstat(filepath.Join(dir, name+".json"))
	return err == nil
}

func appendSpelling(ss []string, s string) []string {
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}
