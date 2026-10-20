package uninstall

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-678
// The uninstall orchestrator: a dry run reports the schedules, config files and sign-in it
// would remove AND the archives it keeps, changing nothing; a real run removes them in the
// safe order; and it NEVER deletes an archive — it refuses outright when the config dir
// overlaps one (R36). Schedule sweep/removal and the sign-in clear are seamed so the test
// touches no real OS scheduler or credential store.
func TestMA678_UninstallOrchestrator(t *testing.T) {
	var removedSched []string
	var clearedTenant string
	origList, origRemove, origClear := listSchedules, removeSchedule, clearSignIn
	defer func() { listSchedules, removeSchedule, clearSignIn = origList, origRemove, origClear }()
	listSchedules = func() []schedule.InstalledEntry {
		return []schedule.InstalledEntry{{Name: "mailarchive-backup", Out: "/a"}, {Name: "mailarchive-backup weekly-verify", Out: "/a"}}
	}
	removeSchedule = func(s schedule.Spec) error { removedSched = append(removedSched, s.Name); return nil }
	clearSignIn = func(tenant, clientID string) error { clearedTenant = tenant; return nil }

	cfg := t.TempDir()
	for _, n := range []string{"desktop-settings.json", "last-archive.txt", "backup.json"} {
		writeFile(t, filepath.Join(cfg, n), "x")
	}
	writeFile(t, filepath.Join(cfg, "graph-config.json"), `{"tenant":"contoso","clientId":"c1"}`)
	kept := t.TempDir() // a known archive, not overlapping cfg
	writeFile(t, filepath.Join(kept, manifestName), `{"version":6,"stores":{}}`)

	// Dry run: reports, changes nothing.
	rep, err := Uninstall(cfg, []string{kept}, "/x/mailarchive", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Schedules) != 2 {
		t.Fatalf("dry-run must list the 2 schedules, got %v", rep.Schedules)
	}
	if len(rep.ConfigFiles) == 0 {
		t.Fatal("dry-run must list config files")
	}
	if len(rep.KeptArchives) != 1 || rep.KeptArchives[0] != kept {
		t.Fatalf("dry-run must name the kept archive, got %v", rep.KeptArchives)
	}
	if !rep.ClearedSignIn {
		t.Fatal("dry-run must note the sign-in is present")
	}
	if len(removedSched) != 0 || clearedTenant != "" {
		t.Fatal("a dry run must change nothing")
	}
	if _, err := os.Stat(filepath.Join(cfg, "desktop-settings.json")); err != nil {
		t.Fatal("a dry run must not delete config files")
	}

	// Execute: removes schedules + sign-in + config; keeps the archive.
	if _, err := Uninstall(cfg, []string{kept}, "/x/mailarchive", false); err != nil {
		t.Fatal(err)
	}
	if len(removedSched) != 2 {
		t.Fatalf("execute must remove both schedules, got %v", removedSched)
	}
	if clearedTenant != "contoso" {
		t.Fatalf("execute must clear the sign-in with the keys read from graph-config.json, got %q", clearedTenant)
	}
	if _, err := os.Stat(filepath.Join(cfg, "desktop-settings.json")); !os.IsNotExist(err) {
		t.Fatal("execute must delete the config files")
	}
	if _, err := os.Stat(filepath.Join(kept, manifestName)); err != nil {
		t.Fatal("uninstall must NEVER delete an archive (R36)")
	}

	// Overlap: a config dir holding a manifest refuses, and deletes nothing.
	cfg2 := t.TempDir()
	writeFile(t, filepath.Join(cfg2, manifestName), `{"version":6,"stores":{}}`)
	writeFile(t, filepath.Join(cfg2, "desktop-settings.json"), "x")
	if _, err := Uninstall(cfg2, nil, "/x", false); err == nil {
		t.Fatal("uninstall must refuse a config dir that overlaps an archive (R36)")
	}
	if _, err := os.Stat(filepath.Join(cfg2, "desktop-settings.json")); err != nil {
		t.Fatal("a refused uninstall must delete nothing")
	}
}

// covers: MA-681
// Uninstall fails CLOSED when graph-config.json is present but unreadable/corrupt (a
// non-not-found error): it stops before removing anything and names the remedy, so the saved
// Microsoft sign-in (client secret / device token) is never stranded by deleting the only
// record of its keys while the clear is silently skipped (U-C4). A legitimately ABSENT config
// is not an error (nothing to clear). Encodes the 2026-10-20 security-pass finding.
func TestMA681_UninstallFailsClosedOnUnreadableGraphConfig(t *testing.T) {
	origList, origRemove, origClear := listSchedules, removeSchedule, clearSignIn
	defer func() { listSchedules, removeSchedule, clearSignIn = origList, origRemove, origClear }()
	var cleared bool
	listSchedules = func() []schedule.InstalledEntry { return nil }
	removeSchedule = func(s schedule.Spec) error { return nil }
	clearSignIn = func(tenant, clientID string) error { cleared = true; return nil }

	// A corrupt (truncated) graph-config.json present alongside a normal config file.
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "graph-config.json"), `{"tenant":"contoso",`)
	writeFile(t, filepath.Join(cfg, "desktop-settings.json"), "x")

	if _, err := Uninstall(cfg, nil, "/x/mailarchive", false); err == nil {
		t.Fatal("uninstall must FAIL CLOSED when the sign-in config is present but unreadable (U-C4)")
	}
	if _, err := os.Stat(filepath.Join(cfg, "graph-config.json")); err != nil {
		t.Fatal("a fail-closed uninstall must NOT delete graph-config.json (it records the keys needed to clear the secret)")
	}
	if _, err := os.Stat(filepath.Join(cfg, "desktop-settings.json")); err != nil {
		t.Fatal("a fail-closed uninstall must delete nothing")
	}
	if cleared {
		t.Fatal("the clear must not run when the keys could not be read")
	}

	// Positive twin: a legitimately ABSENT config is not an error — the run proceeds.
	cfg2 := t.TempDir()
	writeFile(t, filepath.Join(cfg2, "desktop-settings.json"), "x")
	if _, err := Uninstall(cfg2, nil, "/x/mailarchive", false); err != nil {
		t.Fatalf("an absent graph-config.json must not be an error: %v", err)
	}
}
