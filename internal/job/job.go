// Package job is the GUI's scheduled-job file: the wizard's answers, persisted
// so the headless run (`mailarchive-gui -job FILE`) can repeat them without any
// dialog (design-schedule-v2 P7/S11). The Task Scheduler run string stays
// short — the path to this file — whatever the inputs are.
package job

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Version of the job file format. Unknown fields are ignored on read (additive
// evolution, like the manifest); a file newer than this program is refused.
const Version = 1

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
}

// Dir is the per-user directory job files live in.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mailarchive"), nil
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
	data, err := os.ReadFile(path)
	if err != nil {
		return j, fmt.Errorf("job file %s: %w", path, err)
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
