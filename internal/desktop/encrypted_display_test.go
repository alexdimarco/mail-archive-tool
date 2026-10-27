package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
)

// The Desktop's display of an encrypted archive, before any key (the P3-3 plan, 3b;
// MA-804).

// recordedArchive makes an encrypted archive this computer records.
func recordedArchive(t *testing.T, dir, name string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	p, err := keyring.Create(out, "desktop test passphrase", keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	return out
}

// covers: MA-804, MA-739, R37
// The Danger zone's uninstall (MA-739, K8, A20): the plan's text lists leftover
// working copies when there are any — the page's script reads them, which it never
// did — and says this computer's record of encrypted archives and its remembered
// keys are kept, only when there is one; an encrypted archive this computer recorded
// at the command line, in no Desktop index, is among the archives kept and guarded.
func TestTheUninstallPlanNamesScratchAndTheRecord(t *testing.T) {
	// The page's script is not run by go test: its lines are pinned (a live check
	// drives the page itself).
	if !strings.Contains(dashboardJS, `if(scratch.length){lines.push("- leftover working copies in MailArchive's working folder: "+scratch.length)`) ||
		!strings.Contains(dashboardJS, "if(d.encryptedRecordKept)lines.push(") {
		t.Fatal("the plan's text does not render scratch files or the kept record")
	}
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	d := newDashboard(Config{ConfigPath: filepath.Join(t.TempDir(), "graph-config.json")})
	if got := d.knownArchivePaths(); len(got) != 0 || encryptedRecordKept() {
		t.Fatalf("positive twin, no record: %v", got)
	}
	// The record keeps the folder fully resolved, in one letter case where paths fold.
	recordedArchive(t, t.TempDir(), "Cli")
	got := d.knownArchivePaths()
	assure.Reached(t, encryptedRecordKept() && len(got) == 1 && strings.EqualFold(filepath.Base(got[0]), "Cli"), "the recorded archive kept")
}

// covers: MA-804, MA-731, R37
// The page says an encrypted archive is one: its row in Your archives carries the
// "encrypted" label and the Overview the encrypted kind line — what MA-731's list
// check never rendered. A recorded encrypted folder that no longer holds its archive
// (its key files gone) is greyed with its case's own words and no Switch or Pause,
// never a normal archive. The positive twin: a plaintext local archive keeps its
// label.
func TestTheDesktopShowsAnEncryptedArchiveAsOne(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	dir := t.TempDir()
	idxPath := filepath.Join(dir, archindex.Name)
	settings := filepath.Join(dir, "s.json")
	plain := archiveAt(t, dir, "Plain", map[string]string{"path:/mail/a.mbox": "a"})
	enc := recordedArchive(t, dir, "Enc")
	broken := recordedArchive(t, dir, "Broken")
	for _, name := range []string{vault.FileName, vault.BackupName} {
		if err := os.Remove(filepath.Join(broken, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archindex.WriteFile(idxPath, archindex.Index{Entries: []archindex.Entry{
		{Path: plain, AddedAt: at0, LastSeenAt: at0}, {Path: enc, AddedAt: at0, LastSeenAt: at0}, {Path: broken, AddedAt: at0, LastSeenAt: at0},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(settings, Settings{Out: enc}); err != nil {
		t.Fatal(err)
	}
	page, _ := getPage(t, DashboardHandler(Config{SettingsPath: settings, IndexPath: idxPath, GOOS: "darwin"}))
	card := page[strings.Index(page, `id="sec-archives"`):]
	card = card[:strings.Index(card, "</ul>")]
	rowOf := func(path string) string {
		i := strings.Index(card, `<span class="mono archive-path">`+path+`</span>`)
		if i < 0 {
			t.Fatalf("no row for %s:\n%s", path, card)
		}
		row := card[i:]
		if j := strings.Index(row, "</li>"); j >= 0 {
			row = row[:j]
		}
		return row
	}
	if !strings.Contains(rowOf(plain), "local mail files") {
		t.Fatalf("positive twin: the plaintext row:\n%s", rowOf(plain))
	}
	if !strings.Contains(rowOf(enc), `<span class="archive-kind">encrypted</span>`) || !strings.Contains(page, "This archive is encrypted") {
		t.Fatalf("the encrypted row and kind line:\n%s", rowOf(enc))
	}
	row := rowOf(broken)
	li := card[:strings.Index(card, `<span class="mono archive-path">`+broken)]
	li = li[strings.LastIndex(li, "<li "):]
	if !strings.Contains(li, "unavailable") || !strings.Contains(row, "used to hold an encrypted archive") ||
		strings.Contains(row, "data-switch-archive") || strings.Contains(row, "data-pause-archive") {
		t.Fatalf("a recorded folder without its archive:\n%s%s", li, row)
	}
	assure.Reached(t, row, "the broken row rendered")
}
