package uninstall

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
)

// Report is what a full uninstall will remove (a dry run) or did remove, plus — the
// point of the promise — the archives it keeps untouched (R36).
type Report struct {
	Schedules     []string // scheduler entry names removed
	ClearedSignIn bool     // a device sign-in / client secret was present and cleared
	ConfigFiles   []string // config-dir files removed (absolute paths)
	KeptArchives  []string // archives left untouched, named for the owner
	BinaryNote    string   // how to remove the program itself (no self-delete, U-C6)
}

// Seams (overridable in tests, so a test touches no real scheduler or credential store):
// the live schedule sweep, the per-entry removal, and the sign-in clear.
var (
	listSchedules  = schedule.OurInstalled
	removeSchedule = schedule.RemoveLiveEntry
	clearSignIn    = func(tenant, clientID string) error {
		ts := graphconfig.DefaultTokenStore(tenant, clientID)
		tcp, _ := graphconfig.DefaultTokenCachePath(tenant, clientID)
		ss, _ := graphconfig.DefaultSecretStore()
		return ClearCredentials(tenant, clientID, ts, tcp, ss)
	}
)

// Uninstall removes the program's schedules, config and sign-in in the safe order
// (schedules first so nothing keeps running once the program is gone; then the sign-in;
// then the config files — the enumeration sources last), and NEVER touches an archive
// (R36: ConfigTargets refuses on any overlap and names only a fixed allow-list; the
// schedule removal uses RemoveLiveEntry, which keeps the archive-side descriptor). dryRun
// computes the Report without changing anything. On a real run a step's error stops it
// (fail-closed), returning the Report of what had been done.
func Uninstall(configDir string, knownArchives []string, exe string, dryRun bool) (*Report, error) {
	cfgTargets, err := ConfigTargets(configDir, knownArchives)
	if err != nil {
		return nil, err // ErrOverlap or a read error — nothing is touched
	}
	entries := listSchedules()
	tenant, clientID := graphKeys(configDir)

	rep := &Report{
		KeptArchives:  append([]string(nil), knownArchives...),
		BinaryNote:    binaryNote(exe),
		ClearedSignIn: tenant != "" || clientID != "",
		ConfigFiles:   append([]string(nil), cfgTargets...),
	}
	for _, e := range entries {
		rep.Schedules = append(rep.Schedules, e.Name)
	}
	if dryRun {
		return rep, nil
	}

	// 1) Schedules first (U-C5): nothing keeps running after the program is removed.
	for _, e := range entries {
		spec := schedule.Spec{Name: e.Name, Out: e.Out, WrapperPath: schedule.DefaultWrapperPath(e.Name)}
		if rerr := removeSchedule(spec); rerr != nil {
			return rep, fmt.Errorf("could not remove the schedule %q (stopping; nothing else removed): %w", e.Name, rerr)
		}
	}
	// 2) The sign-in (token + client secret). The keys were read ABOVE, so they outlive
	// the config deletion below (U-C4).
	if rep.ClearedSignIn {
		if cerr := clearSignIn(tenant, clientID); cerr != nil {
			return rep, cerr
		}
	}
	// 3) The config files (the enumeration sources, deleted last).
	for _, f := range cfgTargets {
		if derr := os.Remove(f); derr != nil && !errors.Is(derr, fs.ErrNotExist) {
			return rep, fmt.Errorf("could not remove %s: %w", f, derr)
		}
	}
	return rep, nil
}

// graphKeys reads the Graph registration's tenant+clientID from graph-config.json in
// configDir ("" when absent) — BEFORE that file is deleted, so the derived credential
// keys survive the clear (U-C4).
func graphKeys(configDir string) (tenant, clientID string) {
	c, err := graphconfig.Load(filepath.Join(configDir, "graph-config.json"))
	if err != nil || c == nil {
		return "", ""
	}
	return c.Tenant, c.ClientID
}

// binaryNote tells the owner how to remove the program itself; uninstall never deletes a
// running executable (U-C6).
func binaryNote(exe string) string {
	switch runtime.GOOS {
	case "windows":
		return "To remove the program itself, open Settings → Apps → Installed apps (or Add/Remove Programs) and uninstall MailArchive."
	case "darwin":
		return "To remove the program itself, drag MailArchive.app to the Trash."
	default:
		note := "To remove the program itself, delete the mailarchive binary"
		if exe != "" {
			note += " (" + exe + ")"
		}
		return note + "."
	}
}
