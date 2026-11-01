package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-976, R36, S41
// "Save and keep" moves a Microsoft 365 capture's archive location; into a folder that already
// holds files and no archive, its first scheduled capture would be refused by the emptied-folder
// guard (MA-956). Settings refuses it at Save, in the guard's words (design-archive-delete §14,
// slice 2's finding A3-02), and writes nothing: the location, BackupFor and the scheduler's entry
// as they were. Positive twins first: "Save and keep" into an empty folder, and into an archive
// folder, saves.
func TestSaveAndKeepIntoAFolderOfFilesIsRefused(t *testing.T) {
	e := newWsEnv(t)
	c := t.TempDir()
	fake := &backupFake{tasks: map[string]bool{c: true}}
	useBackupFake(e, fake)
	keepTo := func(out string) map[string]any {
		t.Helper()
		if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: c}); err != nil {
			t.Fatal(err)
		}
		_, m := e.post("/api/settings", map[string]any{"out": out, "backup": "keep"})
		return m
	}

	// Positive twins: an empty folder, and an archive's folder.
	empty := t.TempDir()
	if m := keepTo(empty); m["ok"] != true {
		t.Fatalf("Save and keep into an empty folder: %v", m)
	}
	arch := filepath.Join(t.TempDir(), "Arch")
	os.MkdirAll(arch, 0o755)
	buildArchiveAt(t, arch)
	if m := keepTo(arch); m["ok"] != true {
		t.Fatalf("Save and keep into an archive: %v", m)
	}

	// A folder of the person's files.
	mine := t.TempDir()
	os.WriteFile(filepath.Join(mine, "taxes.pdf"), []byte("%PDF"), 0o600)
	m := keepTo(mine)
	msg, _ := m["error"].(string)
	assure.Refused(t, http.StatusOK, msg, assure.Code(http.StatusOK), assure.Names("Nothing was saved", mine, "holds no MailArchive archive", "choose an empty folder"),
		assure.Forbid("taxes.pdf"),
		assure.NoSideEffect(func() bool {
			s := LoadSettings(e.cfg.SettingsPath)
			return s.Out == c && s.BackupFor == "" && fake.tasks[c] && len(fake.removed) == 0
		}))
}
