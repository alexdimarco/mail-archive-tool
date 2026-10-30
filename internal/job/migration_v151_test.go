package job

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The v1.5.1 job types, copied word for word from internal/job at ea6515a (the
// build the fixtures were written by), with only the type names changed (design-
// presync-attended §10, C-39).
type v151Job struct {
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
	// ThunderbirdSync, when present, means this scheduled GUI job syncs
	// Thunderbird before archiving (design §4.2.5). Presence means ON. Additive:
	// a job written before it reads as off.
	ThunderbirdSync *v151ThunderbirdSync `json:"thunderbird_sync,omitempty"`
	// ProgramCopies, "on" or "off", is a new encrypted archive's choice to keep copies
	// of MailArchive in it, as it was made (program design R21): each run carries it
	// until a run seals it, never over a choice already sealed (the PG4 pass, SEC-1).
	// Additive: a job written before it carries none.
	ProgramCopies string `json:"program_copies,omitempty"`
}

type v151ThunderbirdSync struct {
	Profile string `json:"profile,omitempty"`
}

// covers: MA-916
// Job files cross versions both ways (design-presync-attended §10, slice AS7, C-39):
// the attended sync adds no job field, so v1.5.1's own job files (frozen from
// ea6515a) — a plaintext job with the sync, and an encrypted job with the sync,
// program copies, a since date, -copy-first and a followed link — read in today's code
// with every field kept, and today's writer writes them back byte for byte, which
// the copied v1.5.1 type reads with every field kept.
func TestJobFilesCrossVersions(t *testing.T) {
	for _, name := range []string{"job-plain-sync.json", "job-encrypted-sync.json"} {
		t.Run(name, func(t *testing.T) {
			frozen, err := os.ReadFile(filepath.Join("testdata", "v151", name))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, frozen, 0o600); err != nil {
				t.Fatal(err)
			}
			j, err := Read(p)
			if err != nil {
				t.Fatalf("today's reader refuses a v1.5.1 job file: %v", err)
			}
			var old v151Job
			if err := json.Unmarshal(frozen, &old); err != nil {
				t.Fatal(err)
			}
			if j.ThunderbirdSync == nil || old.ThunderbirdSync == nil {
				t.Fatal("fixture: a sync job read with the sync off")
			}
			if j.Name != old.Name || j.Out != old.Out || j.Mode != old.Mode || j.Auto != old.Auto || j.Since != old.Since ||
				j.CopyFirst != old.CopyFirst || j.ProgramCopies != old.ProgramCopies || !reflect.DeepEqual(j.Inputs, old.Inputs) ||
				!reflect.DeepEqual(j.FollowLinks, old.FollowLinks) || j.ThunderbirdSync.Profile != old.ThunderbirdSync.Profile {
				t.Errorf("today's reader lost a field:\n today %+v\n v1.5.1 %+v", j, old)
			}
			again := filepath.Join(dir, "again.json")
			if err := Write(again, j); err != nil {
				t.Fatal(err)
			}
			written, _ := os.ReadFile(again)
			if !bytes.Equal(written, frozen) {
				t.Errorf("today's writer changed a v1.5.1 job file:\n%s\nwant\n%s", written, frozen)
			}
			var back v151Job
			if err := json.Unmarshal(written, &back); err != nil || !reflect.DeepEqual(back, old) {
				t.Errorf("v1.5.1 reads today's job file as %+v (%v), want %+v", back, err, old)
			}
		})
	}
}
