package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-507, MA-531, R29, R12, S52
// Start checks the destination against the archives as they are now (E4-AI1): the preview collected them
// when it was made, and a Settings change in a second tab can make a folder the archive afterwards. An
// export of another archive folder (or of Microsoft 365) previewed into <arch2>/exports, then Settings
// saved with arch2 as the archive location, then Start, used to be written inside the new archive and
// complete ("an export never lands in the archive", R29), because the engine rechecked the archives the
// preview knew and an archive-bound preview is checked for the configured archive only when it was made
// from it. The positive twin: with Settings unchanged the same preview starts and completes.
func TestStartRechecksTheDestinationAgainstTheArchiveSettingsNameNow(t *testing.T) {
	for _, kind := range []string{"another archive folder", "microsoft 365"} {
		t.Run(kind, func(t *testing.T) {
			e := newWsEnv(t)
			arch2 := filepath.Join(t.TempDir(), "arch2") // outside the folder Settings keep their files in
			dest := filepath.Join(arch2, "exports")
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			preview := func() string {
				if kind == "microsoft 365" {
					ids := e.folders()
					sel := e.selection(entry(ids["Sent Items"], false))
					sel["dest"] = dest
					id, _ := e.preview(sel)
					return id
				}
				other := filepath.Join(e.dir, "inherited")
				if _, err := os.Stat(other); err != nil {
					buildArchiveAt(t, other)
				}
				oarch := archSource("archive-other", other)
				e.archFolders(oarch)
				sel := e.archSel(oarch, entry("bob", false))
				sel["dest"] = dest
				id, _ := e.preview(sel)
				return id
			}

			// the positive twin: Settings as they were, the preview starts and completes
			id := preview()
			e.ok("/api/exports/start", map[string]any{"previewId": id})
			if last := e.waitRun()["last"].(map[string]any); last["status"] != "complete" {
				t.Fatalf("the positive twin: %v", last)
			}

			// the same preview, then Settings name arch2 as the archive, then Start
			id = preview()
			e.ok("/api/settings", map[string]any{"Out": arch2})
			before := dirDigest(t, arch2)
			code, out := e.post("/api/exports/start", map[string]any{"previewId": id})
			if code == 200 && out["ok"] == true {
				e.waitRun()
				t.Fatalf("Start wrote an export into %s, the archive location Settings name now: %v", arch2, dirNames(t, arch2))
			}
			if out["id"] != "R-f" || !strings.Contains(out["error"].(string), "inside your archive") || !strings.Contains(out["error"].(string), arch2) {
				t.Errorf("Start's refusal: %d %v, want R-f naming %s", code, out, arch2)
			}
			if after := dirDigest(t, arch2); after != before {
				t.Errorf("a refused Start changed %s", arch2)
			}
		})
	}
}

// dirNames lists the entries of dir, for a message.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range ents {
		out = append(out, en.Name())
	}
	return out
}
