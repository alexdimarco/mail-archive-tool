// Package job is the GUI's scheduled-job file: the wizard's answers, persisted
// so the headless run (`mailarchive-gui -job FILE`) can repeat them without any
// dialog (design-schedule-v2 P7/S11). The Task Scheduler run string stays
// short — the path to this file — whatever the inputs are.
package job

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Version of the job file format. Unknown fields are ignored on read (additive
// evolution, like the manifest); a file newer than this program is refused.
const Version = 1

// maxJobFileSize bounds the job file a headless run will read: it is owner-
// written JSON of a few hundred bytes, so anything far larger is not a real job
// file and is refused rather than slurped (the oversize refusal, MA-640).
const maxJobFileSize = 64 * 1024

// Job is one scheduled GUI export.
type Job struct {
	Version         int      `json:"version"`
	Name            string   `json:"name"`
	Inputs          []string `json:"inputs,omitempty"`
	Auto            bool     `json:"auto,omitempty"` // re-discover stores at run time
	Out             string   `json:"out"`
	Mode            string   `json:"mode"` // incremental | full
	Since           string   `json:"since,omitempty"`
	CopyFirst       bool     `json:"copy_first,omitempty"`
	Outlook         bool     `json:"outlook,omitempty"`
	OutlookSyncWait string   `json:"outlook_sync_wait,omitempty"`
	KeepRaw         bool     `json:"raw,omitempty"`
	// FollowLinks names the inputs whose directory links (symlinks, on Windows
	// junctions) the run reads: the per-input opt-in of owner ruling OR6, the wizard's
	// "Follow folder links" answer (the CLI's -follow-links). Additive: a job written
	// before it reads as no opt-in.
	FollowLinks []string `json:"follow_links,omitempty"`
}

// Dir is the per-user directory job files live in.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mailarchive"), nil
}

// LastArchiveFile is the file the GUI wizard remembers the archive it last worked on in.
func LastArchiveFile() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "last-archive.txt"), nil
}

// ReadLastArchive is the archive folder the file at path names, or "" when it cannot be read.
// It only reads: whether the folder is still an archive is the caller's check.
func ReadLastArchive(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// DesktopSettingsFile is MailArchive Desktop's preferences file, beside the job files
// and last-archive.txt under the per-user config dir. The wizard reads one field of it —
// the active archive — so the two surfaces agree on which archive is current
// (design-archive-manager §13 UX-2).
func DesktopSettingsFile() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "desktop-settings.json"), nil
}

// ReadDesktopOut is the Desktop's active archive location — the "out" field of its
// settings file — or "" when the file is missing, unreadable, or sets none. It reads
// only that one field, so it does not depend on the rest of the Desktop's schema and
// cannot drift as that schema grows (a newer build's extra keys are ignored).
func ReadDesktopOut(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var s struct {
		Out string `json:"out"`
	}
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s.Out)
}

// PathFor is the job file for a schedule name.
func PathFor(name string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name+".json"), nil
}

// Write persists j atomically, readable by the owner only.
func Write(path string, j Job) error {
	j.Version = Version
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".job-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
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

// Read loads a job file, refusing one written by a newer program and one that
// lacks the essentials (out, mode).
func Read(path string) (Job, error) {
	var j Job
	data, err := readSecure(path)
	if err != nil {
		return j, err
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return j, fmt.Errorf("job file %s is unreadable (%v): re-create the schedule from the wizard", path, err)
	}
	if j.Version > Version {
		return j, fmt.Errorf("job file %s was written by a newer mailarchive (version %d, this program reads %d): upgrade, or re-create the schedule from this wizard", path, j.Version, Version)
	}
	if j.Out == "" {
		return j, errors.New("job file " + path + " names no output folder: re-create the schedule from the wizard")
	}
	if j.Mode == "" {
		j.Mode = "incremental"
	}
	return j, nil
}

// readSecure reads a job file for a headless (unattended) run, which trusts its
// contents, so a planted file in the owner's config dir is refused rather than
// followed or slurped: a final-component symbolic link is refused and its target
// left untouched; a non-regular file (a FIFO, device, directory or socket) is
// refused — caught by the Lstat BEFORE any open, so a FIFO cannot hang the read;
// a file another account can change is refused (a scheduled job is owner-only:
// the POSIX mode bits on unix, the security descriptor via internal/winsec on
// Windows, since Go reports mode 0666 for every writable file there); and the read
// is bounded by maxJobFileSize (R12, MA-640). Residual TOCTOU between the stat and
// the open is the adversarial pass's concern.
func readSecure(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("job file %s: %w", path, err)
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return nil, fmt.Errorf("job file %s is a symbolic link: refusing to follow it; re-create the schedule from the wizard", path)
	case !fi.Mode().IsRegular():
		return nil, fmt.Errorf("job file %s is not a regular file (%s): refusing to read it; re-create the schedule from the wizard", path, fi.Mode().Type())
	case fi.Size() > maxJobFileSize:
		return nil, fmt.Errorf("job file %s is %d bytes, over the %d-byte limit: refusing to read it", path, fi.Size(), maxJobFileSize)
	}
	if err := ownerOnly(fi, path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("job file %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxJobFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("job file %s: %w", path, err)
	}
	if int64(len(data)) > maxJobFileSize {
		return nil, fmt.Errorf("job file %s is over the %d-byte limit: refusing to read it", path, maxJobFileSize)
	}
	return data, nil
}
