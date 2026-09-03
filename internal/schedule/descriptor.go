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
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(out, DescriptorName)
	tmp, err := os.CreateTemp(out, ".mailarchive-schedule-*.tmp")
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
	var d Descriptor
	data, err := os.ReadFile(filepath.Join(out, DescriptorName))
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf("schedule descriptor %s is unreadable (%v): re-run `mailarchive schedule ... -install` to rewrite it", filepath.Join(out, DescriptorName), err)
	}
	// The file is untrusted (any writer of the archive directory could edit
	// it) and its strings are echoed into terminals and dialogs: strip control
	// characters, and hold the name and cadence to their grammars.
	d.Name = clean(d.Name)
	d.Exe, d.Host, d.Wrapper, d.Log = clean(d.Exe), clean(d.Host), clean(d.Wrapper), clean(d.Log)
	for i := range d.Job {
		d.Job[i] = clean(d.Job[i])
	}
	if _, err := SanitizeName(d.Name); err != nil {
		return d, fmt.Errorf("schedule descriptor %s names an invalid schedule (%v): re-run `mailarchive schedule ... -install` to rewrite it", filepath.Join(out, DescriptorName), err)
	}
	if iv, err := ParseInterval(clean(d.Interval)); err == nil {
		d.Interval = string(iv)
	} else {
		d.Interval = string(Daily)
	}
	if _, _, err := parseHHMM(clean(d.At)); err != nil {
		d.At = "??:??"
	} else {
		d.At = clean(d.At)
	}
	return d, nil
}

// clean strips control characters from an untrusted string.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// RemoveDescriptor deletes the descriptor; a missing file is not an error.
func RemoveDescriptor(out string) error {
	err := os.Remove(filepath.Join(out, DescriptorName))
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
