package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
	// Presync records that this schedule syncs Thunderbird before archiving, for
	// status display and judgement only — the run never reads its settings from
	// here (P6). Additive: absent on schedules without the feature.
	Presync *PresyncDesc `json:"presync,omitempty"`
}

// PresyncDesc is a schedule's Thunderbird pre-sync descriptor (design §4.2.3):
// what the schedule says it does, so status can show and judge it.
type PresyncDesc struct {
	Kind    string `json:"kind"` // "thunderbird"
	Profile string `json:"profile,omitempty"`
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
	d.Presync = presyncFromArgs(s.Args)
	return d
}

// presyncFromArgs reads a schedule's Thunderbird pre-sync setting from the baked
// job argv (design §4.2.3): -thunderbird-sync enables it and -thunderbird-profile
// names the profile. It is for status display and judgement only — the run never
// reads its setting from the descriptor (P6). Returns nil when the sync is off.
func presyncFromArgs(args []string) *PresyncDesc {
	on, profile := false, ""
	for i, a := range args {
		switch a {
		case "-thunderbird-sync", "--thunderbird-sync":
			on = true
		case "-thunderbird-profile", "--thunderbird-profile":
			if i+1 < len(args) {
				profile = args[i+1]
			}
		}
	}
	if !on {
		return nil
	}
	return &PresyncDesc{Kind: "thunderbird", Profile: profile}
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
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name()) // a failed write leaves nothing in the archive (G8-AC3)
		return err
	}
	return nil
}

// descriptorFileFor is the descriptor file an install of s writes: the verify's
// own for a verify job, the backup's for any other.
func descriptorFileFor(s Spec) string {
	if isVerifyJob(s) {
		return VerifyDescriptorName
	}
	return DescriptorName
}

// descriptorPathUsable refuses a path a descriptor write could never replace: a
// directory (whoever handed the folder over can plant one — the rename over it
// fails on every retry). A missing path, a file or a symlink (replaced, never
// followed) is fine.
func descriptorPathUsable(path string) error {
	if fi, err := os.Lstat(path); err == nil && fi.IsDir() {
		return fmt.Errorf("%s is a directory, so the archive's schedule cannot be recorded there — nothing was installed; move or delete it and install again", path)
	}
	return nil
}

// ReadDescriptor reads the archive's descriptor. A missing file is reported
// with os.ErrNotExist (a legible "no schedule recorded here" for callers).
func ReadDescriptor(out string) (Descriptor, error) {
	return readDescriptorFile(out, DescriptorName, "schedule descriptor", UnreadableDescriptorRemedy)
}

// ReadVerifyDescriptor reads the verify schedule's descriptor under the same
// untrusted-input rules as ReadDescriptor; a missing file is os.ErrNotExist (no
// verify recorded here — none installed, or one installed by a release that
// wrote no verify descriptor). What it records decides which entry -remove
// takes for this archive's verify, what the -remove hint names and what status
// judges, so it is trusted only as a verify of THIS archive under a name of its
// own. A job that is not a verify (a backup's descriptor copied over it), a
// verify recorded on this host for another folder (an archive copied with its
// records), or the name this archive's backup descriptor records (the install
// refuses a verify under it, so one of the two is not what it says) is an error
// like an unreadable file: status names it with its remedy, the verify's
// removal never deletes it, and it never turns `-out X -remove` into a verify
// removal. A verify recorded on another host may name the folder as that host
// mounts it, so its -out is not held to this host's spelling.
func ReadVerifyDescriptor(out string) (Descriptor, error) {
	d, err := readDescriptorFile(out, VerifyDescriptorName, "verify schedule descriptor", UnreadableVerifyDescriptorRemedy)
	if err != nil {
		return d, err
	}
	path := filepath.Join(out, VerifyDescriptorName)
	if !descriptorIsVerify(d) {
		what := "an empty job"
		if len(d.Job) > 0 {
			what = fmt.Sprintf("a job starting %q", d.Job[0])
		}
		return d, fmt.Errorf("verify schedule descriptor %s records %s, not a verify: %s", path, what, UnreadableVerifyDescriptorRemedy)
	}
	if host, _ := os.Hostname(); d.Host == "" || host == "" || d.Host == host {
		// A verify recorded for a folder that no longer exists is this archive's own
		// record from before it was renamed on this host (its entry still runs
		// against the old path), so it stays visible; one for a folder that is
		// still there is that archive's record, copied.
		if jo := jobOut(d.Job); !sameDir(jo, out) && (jo == "" || !errors.Is(statErr(jo), os.ErrNotExist)) {
			if jo == "" {
				jo = "no -out"
			}
			return d, fmt.Errorf("verify schedule descriptor %s records a verify of %s, not of this archive: %s", path, jo, UnreadableVerifyDescriptorRemedy)
		}
	}
	if err := bindDescriptorName(out, d, path); err != nil {
		return d, fmt.Errorf("%w: %s", err, UnreadableVerifyDescriptorRemedy)
	}
	if b, berr := ReadDescriptor(out); berr == nil && !descriptorIsVerify(b) && b.Name == d.Name {
		return d, fmt.Errorf("verify schedule descriptor %s records %q, the name of this archive's backup schedule — the install refuses a verify under the backup's name, so this record is not the verify's: %s", path, d.Name, UnreadableVerifyDescriptorRemedy)
	}
	return d, nil
}

func statErr(p string) error { _, err := os.Stat(p); return err }

// jobOut returns the -out a recorded job carries (`-out V` or `-out=V`, one
// dash or two), or "".
func jobOut(job []string) string {
	for i := 0; i < len(job); i++ {
		if !strings.HasPrefix(job[i], "-") {
			continue
		}
		a := strings.TrimLeft(job[i], "-")
		if a == "out" && i+1 < len(job) {
			return job[i+1]
		}
		if strings.HasPrefix(a, "out=") {
			return a[len("out="):]
		}
	}
	return ""
}

// sameDir reports whether two spellings name the same directory: equal once
// made absolute and cleaned, the same file when both stat, or — when one cannot
// be statted — equal ignoring case on the case-insensitive darwin and windows
// volumes.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	abs := func(p string) string {
		if ap, err := filepath.Abs(p); err == nil {
			return ap
		}
		return filepath.Clean(p)
	}
	if abs(a) == abs(b) {
		return true
	}
	fa, ea := os.Stat(a)
	fb, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(fa, fb)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.EqualFold(abs(a), abs(b))
	}
	return false
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
