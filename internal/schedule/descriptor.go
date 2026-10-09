package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// DescriptorName is the archive-local file that links an archive to the
// schedule that feeds it (design-schedule-v2 S5): `status` reads the schedule's
// real name, cadence, host and executable from here instead of guessing, and
// `schedule -out DIR -remove` finds the entry to remove.
const DescriptorName = ".mailarchive-schedule.json"

// VerifyDescriptorName is the archive-local file that records the archive's
// scheduled verify (owner ruling FB-4): a verify is its own scheduler entry, so
// it gets its own descriptor beside the backup's, written when the verify is
// installed and removed only with that entry. `status` judges the last verify's
// staleness against the cadence recorded here, and the install-time name guard
// reads it with the backup's. Same format and write discipline as
// DescriptorName.
const VerifyDescriptorName = ".mailarchive-verify-schedule.json"

// Descriptor describes an installed schedule.
type Descriptor struct {
	Version     int       `json:"version"`
	Name        string    `json:"name"`
	Interval    string    `json:"interval"`
	At          string    `json:"at"`
	Exe         string    `json:"exe"` // the real program (never the wrapper)
	ExeSize     int64     `json:"exe_size,omitempty"`
	ExeMTime    time.Time `json:"exe_mtime,omitempty"`
	Wrapper     string    `json:"wrapper,omitempty"` // Windows wrapper path, when used
	Job         []string  `json:"job"`               // the job's arguments
	Log         string    `json:"log,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
	Host        string    `json:"host"`
}

// DescribeSpec builds the descriptor for a spec, recording the executable's
// identity so a moved or upgraded binary can be detected later.
func DescribeSpec(s Spec) Descriptor {
	d := Descriptor{
		Version: 1, Name: s.Name, Interval: string(s.Interval), At: s.At, Exe: s.Exe,
		Job: append([]string(nil), s.Args...), Log: s.Log, InstalledAt: time.Now().UTC(),
	}
	if s.Wrapper {
		d.Wrapper = s.WrapperPath
	}
	if fi, err := os.Stat(s.Exe); err == nil {
		d.ExeSize, d.ExeMTime = fi.Size(), fi.ModTime().UTC()
	}
	d.Host, _ = os.Hostname()
	return d
}

// WriteDescriptor writes the descriptor into the archive directory atomically.
func WriteDescriptor(out string, d Descriptor) error {
	return writeDescriptorFile(out, DescriptorName, d)
}

// WriteVerifyDescriptor writes the verify schedule's descriptor into the archive
// directory, exactly as WriteDescriptor writes the backup's.
func WriteVerifyDescriptor(out string, d Descriptor) error {
	return writeDescriptorFile(out, VerifyDescriptorName, d)
}

// writeDescriptorFile is the one writer of both descriptors: a temp file in the
// archive directory (created 0600 — owner-only on POSIX — by os.CreateTemp) is
// written, closed and renamed over name, so a reader sees the old file or the
// new one, never a torn one, and the file keeps that mode. The temp name keeps
// the `.mailarchive-*.tmp` shape the orphan sweep (export.SweepOrphans) removes
// if a crash leaves one.
func writeDescriptorFile(out, name string, d Descriptor) error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(out, name)
	tmp, err := os.CreateTemp(out, strings.TrimSuffix(name, ".json")+"-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ReadDescriptor reads the archive's descriptor. A missing file is reported
// with os.ErrNotExist (a legible "no schedule recorded here" for callers).
func ReadDescriptor(out string) (Descriptor, error) {
	return readDescriptorFile(out, DescriptorName, "schedule descriptor", UnreadableDescriptorRemedy)
}

// ReadVerifyDescriptor reads the verify schedule's descriptor under the same
// untrusted-input rules as ReadDescriptor; a missing file is os.ErrNotExist (no
// verify recorded here — none installed, or one installed by a release that
// wrote no verify descriptor).
func ReadVerifyDescriptor(out string) (Descriptor, error) {
	return readDescriptorFile(out, VerifyDescriptorName, "verify schedule descriptor", UnreadableVerifyDescriptorRemedy)
}

// readDescriptorFile is the one reader of both descriptors. what names the file
// in its errors and remedy is what to do about one that cannot be read.
func readDescriptorFile(out, name, what, remedy string) (Descriptor, error) {
	var d Descriptor
	path := filepath.Join(out, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf("%s %s is unreadable (%v): %s", what, path, err, remedy)
	}
	// The file is untrusted (any writer of the archive directory could edit
	// it) and its strings are echoed into terminals and dialogs: strip control
	// characters (via the shared util.StripControl choke-point helper), and hold
	// the name and cadence to their grammars.
	d.Name = util.StripControl(d.Name)
	d.Exe, d.Host, d.Wrapper, d.Log = util.StripControl(d.Exe), util.StripControl(d.Host), util.StripControl(d.Wrapper), util.StripControl(d.Log)
	for i := range d.Job {
		d.Job[i] = util.StripControl(d.Job[i])
	}
	if _, err := SanitizeName(d.Name); err != nil {
		return d, fmt.Errorf("%s %s names an invalid schedule (%v): %s", what, path, err, remedy)
	}
	if iv, err := ParseInterval(util.StripControl(d.Interval)); err == nil {
		d.Interval = string(iv)
	} else {
		d.Interval = string(Daily)
	}
	if _, _, err := parseHHMM(util.StripControl(d.At)); err != nil {
		d.At = "??:??"
	} else {
		d.At = util.StripControl(d.At)
	}
	return d, nil
}

// RemoveDescriptor deletes the descriptor; a missing file is not an error.
func RemoveDescriptor(out string) error {
	return removeDescriptorFile(out, DescriptorName)
}

// RemoveVerifyDescriptor deletes the verify schedule's descriptor; a missing
// file is not an error.
func RemoveVerifyDescriptor(out string) error {
	return removeDescriptorFile(out, VerifyDescriptorName)
}

func removeDescriptorFile(out, name string) error {
	err := os.Remove(filepath.Join(out, name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// DefaultNameFor derives the schedule name for an archive: one schedule per
// archive by default, on every surface (CLI and GUI agree), so a second
// archive never overwrites the first's entry and a re-install of the same
// archive replaces its own.
func DefaultNameFor(out string) string {
	abs := out
	if a, err := filepath.Abs(out); err == nil {
		abs = a
	}
	return "mailarchive-" + util.HashHex(filepath.ToSlash(abs), 8)
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// MaxNameLen bounds -name: it becomes a file name (wrapper, log) and part of
// a Task Scheduler run string.
const MaxNameLen = 40

// SanitizeName validates an operator-supplied schedule name.
func SanitizeName(name string) (string, error) {
	if name == "" {
		return "", errors.New("schedule name is empty")
	}
	if len(name) > MaxNameLen {
		return "", fmt.Errorf("invalid -name %q: at most %d characters (it names files and a scheduler entry)", name, MaxNameLen)
	}
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("invalid -name %q: use letters, digits, '.', '_' and '-' only", name)
	}
	return name, nil
}
