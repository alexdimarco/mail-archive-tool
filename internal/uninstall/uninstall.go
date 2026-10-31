package uninstall

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/progcache"
	"mail-archive-tool/internal/schedule"
)

// Report is what a full uninstall will remove (a dry run) or did remove, plus — the
// point of the promise — the archives it keeps untouched (R36).
type Report struct {
	Schedules     []string // scheduler entry names removed
	ClearedSignIn bool     // a device sign-in / client secret was present and cleared
	// ConfigFiles are the settings files removed (absolute paths): the config dir's, then
	// the Thunderbird sync's state files and their folder (presyncjob.StateDir).
	ConfigFiles []string
	// Scratch is what of MailArchive's working folder (job.ScratchDir) is removed: an
	// encrypted archive's leftover working copies and their folders. Empty — and so not
	// shown — when there is none.
	Scratch []string
	// ProgramCache is the folder of downloaded MailArchive release copies (program
	// design §5, PG2), shown as one folder; programCache is what of it is removed.
	ProgramCache string
	programCache []string
	KeptArchives []string // archives left untouched, named for the owner
	BinaryNote   string   // how to remove the program itself (no self-delete, U-C6)
}

// Seams (overridable in tests, so a test touches no real scheduler or credential store):
// the live schedule sweep, the per-entry removal, and the sign-in clear.
var (
	listSchedules  = schedule.OurInstalled
	removeSchedule = schedule.RemoveLiveEntry
	// programCacheRoot is the downloaded release copies' cache (progcache.DefaultRoot).
	programCacheRoot = progcache.DefaultRoot
	clearSignIn      = func(tenant, clientID string) error {
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
	var scratch []string
	if root, rerr := scratchRoot(); rerr == nil {
		if scratch, err = ScratchTargets(root, knownArchives); err != nil {
			return nil, err // ErrOverlap, ErrScratchUnexpected or a read error — nothing is touched
		}
	}
	// The Thunderbird sync's state folder (presyncjob.StateDir, per platform): its named
	// files, then the folder (design-presync-attended §5.5, C-40, C-63).
	var presyncState []string
	if dir, derr := presyncStateDir(); derr == nil && dir != "" {
		if presyncState, err = PresyncStateTargets(dir, knownArchives); err != nil {
			return nil, err // ErrOverlap or a read error — nothing is touched
		}
	}
	var cache []string
	if root, rerr := programCacheRoot(); rerr == nil {
		if cache, err = progcache.UninstallTargets(root); err != nil {
			return nil, err // a download under way, or a planted link — nothing is touched
		}
	}
	entries := listSchedules()
	tenant, clientID, gerr := graphKeys(configDir)
	if gerr != nil {
		return nil, fmt.Errorf("could not read the Microsoft sign-in settings at %s, so uninstall stopped before removing anything: clear the sign-in by hand (open MailArchive and sign out, or delete that file together with the saved token and app secret) and re-run — otherwise the saved sign-in would be left behind while its settings file is deleted (U-C4): %w", filepath.Join(configDir, "graph-config.json"), gerr)
	}

	rep := &Report{
		KeptArchives:  append([]string(nil), knownArchives...),
		BinaryNote:    binaryNote(exe),
		ClearedSignIn: tenant != "" || clientID != "",
		ConfigFiles:   append(append([]string(nil), cfgTargets...), presyncState...),
		Scratch:       scratch,
		programCache:  cache,
	}
	if len(cache) > 0 {
		rep.ProgramCache = cache[len(cache)-1]
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
	// 3) MailArchive's working folder: an encrypted archive's leftover working copies,
	// deepest first, each folder only when empty (os.Remove, never RemoveAll).
	for _, p := range scratch {
		if derr := os.Remove(p); derr != nil && !errors.Is(derr, fs.ErrNotExist) {
			return rep, fmt.Errorf("could not remove %s from MailArchive's working folder (stopping; the settings were not removed): %w", p, derr)
		}
	}
	// 3b) The downloaded MailArchive release copies: the cache's own files, deepest
	// first, each folder only when empty.
	for _, p := range cache {
		if derr := os.Remove(p); derr != nil && !errors.Is(derr, fs.ErrNotExist) {
			return rep, fmt.Errorf("could not remove %s from MailArchive's program cache (stopping; the settings were not removed): %w", p, derr)
		}
	}
	// 3c) The Thunderbird sync's state: its named files, then the folder, each with
	// os.Remove (the folder only when empty; PresyncStateTargets names it only then).
	for _, p := range presyncState {
		if derr := os.Remove(p); derr != nil && !errors.Is(derr, fs.ErrNotExist) {
			return rep, fmt.Errorf("could not remove %s from the Thunderbird sync's folder (stopping; the settings were not removed): %w", p, derr)
		}
	}
	// 4) The config files (the enumeration sources, deleted last).
	for _, f := range cfgTargets {
		if derr := os.Remove(f); derr != nil && !errors.Is(derr, fs.ErrNotExist) {
			return rep, fmt.Errorf("could not remove %s: %w", f, derr)
		}
	}
	return rep, nil
}

// graphKeys reads the Graph registration's tenant+clientID from graph-config.json in
// configDir — BEFORE that file is deleted, so the derived credential keys survive the
// clear (U-C4). A legitimately ABSENT config yields empty keys with no error (nothing to
// clear). A config that is PRESENT but cannot be read or parsed is a non-not-found error:
// the caller MUST fail closed on it (U-C4), never silently skip the clear and then delete
// the only record of the keys — that would strand the client secret while reporting success.
func graphKeys(configDir string) (tenant, clientID string, err error) {
	c, lerr := graphconfig.Load(filepath.Join(configDir, "graph-config.json"))
	if lerr != nil {
		if errors.Is(lerr, os.ErrNotExist) {
			return "", "", nil // no config yet: nothing to clear
		}
		return "", "", lerr
	}
	if c == nil {
		return "", "", nil
	}
	return c.Tenant, c.ClientID, nil
}

// binaryNote tells the owner how to remove the program itself; uninstall never deletes a
// running executable (U-C6).
func binaryNote(exe string) string { return binaryNoteFor(runtime.GOOS, exe) }

// binaryNoteFor is binaryNote for goos. On macOS it names the bundle as it is built
// and installed, "Mail Archive.app" (packaging/macos/build-app.sh).
func binaryNoteFor(goos, exe string) string {
	switch goos {
	case "windows":
		return "To remove the program itself, open Settings → Apps → Installed apps (or Add/Remove Programs) and uninstall MailArchive."
	case "darwin":
		return "To remove the program itself, drag Mail Archive.app (in your Applications folder, or wherever you put it) to the Trash."
	default:
		note := "To remove the program itself, delete the mailarchive binary"
		if exe != "" {
			note += " (" + exe + ")"
		}
		return note + "."
	}
}
