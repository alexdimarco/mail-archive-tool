package desktop

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// plainMboxArchive archives an mbox folder that is NOT inside a Thunderbird profile (no
// prefs.js above it): a local archive that was not made from Thunderbird.
func plainMboxArchive(t *testing.T) string {
	t.Helper()
	store := filepath.Join(t.TempDir(), "mail")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From - Mon Mar  3 09:00:00 2025\nFrom: a@example.com\nTo: b@example.com\nSubject: hi\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <plain1@ex>\n\nbody\n\n"
	if err := os.WriteFile(filepath.Join(store, "Inbox"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "arch")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{store}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	return out
}

// unchanged snapshots what a refused local install must leave as it was: the job file, the
// descriptor, the settings file and the number of scheduler installs.
type unchanged struct {
	g                   *localRig
	jobB, descB, settsB []byte
	installs            int
	jobPath, descPath   string
	settingsPath        string
}

func snapshot(g *localRig) unchanged {
	u := unchanged{g: g, jobPath: g.jobPath, descPath: filepath.Join(g.out, schedule.DescriptorName), settingsPath: g.settings}
	u.jobB, u.descB, u.settsB, u.installs = fileBytes(u.jobPath), fileBytes(u.descPath), fileBytes(u.settingsPath), len(g.installs)
	return u
}

func (u unchanged) holds() bool {
	return bytes.Equal(u.jobB, fileBytes(u.jobPath)) && bytes.Equal(u.descB, fileBytes(u.descPath)) &&
		bytes.Equal(u.settsB, fileBytes(u.settingsPath)) && u.installs == len(u.g.installs)
}

// covers: MA-860, R37
// Sync is refused, not dropped, off macOS and Windows (section 6, C-10): before, the request
// was dropped silently and the reply said ok. Positive twin first: on macOS it installs with
// the sync on.
func TestSyncOnIsRefusedOffMacOSAndWindows(t *testing.T) {
	out := tbProfileArchive(t)
	mac := newLocalRig(t, out, "darwin")
	if body := mac.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin on macOS: %s", body)
	}
	if j, err := job.Read(mac.jobPath); err != nil || j.ThunderbirdSync == nil {
		t.Fatalf("the positive twin wrote no sync: %v", err)
	}
	os.Remove(mac.jobPath)

	lin := newLocalRig(t, out, "linux")
	if err := SaveSettings(lin.settings, Settings{Interval: "weekly", WeeklyTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	u := snapshot(lin)
	body := lin.post(`{"interval":"daily","time":"04:00","presync":true}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("Thunderbird sync works on macOS and Windows only, so nothing was changed."),
		assure.NoSideEffect(u.holds))
	if fileBytes(lin.jobPath) != nil {
		t.Error("the refused install left a job file")
	}
}

// covers: MA-860, R37
// Sync asked for an archive that was not made from Thunderbird is refused in plain words, with
// the job file, descriptor, settings and scheduler untouched (section 6). Positive twin first:
// the same archive's backup without the sync installs.
func TestSyncOnIsRefusedForAnArchiveNotFromThunderbird(t *testing.T) {
	g := newLocalRig(t, plainMboxArchive(t), "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":false}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin, no sync: %s", body)
	}
	u := snapshot(g)
	body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("wasn't made from a Thunderbird profile on this computer", "Nothing was changed."),
		assure.NoSideEffect(u.holds))
}

// covers: MA-860, MA-858, R37
// Switching the sync on while its profile is not on this computer is refused with the reason
// (C-10), nothing changed; a sync already on and posted on again is not refused (nothing is
// switched), with the profile away too — an edit needs only its job file (MA-858). Positive
// twin first: with the profile here it switches on.
func TestSyncOnIsRefusedWhileTheProfileIsAway(t *testing.T) {
	out := tbProfileArchive(t)
	store := archivedStore(t, out)
	profile := filepath.Dir(filepath.Dir(store)) // <profile>/Mail/Local Folders
	g := newLocalRig(t, out, "windows")
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":false}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create without sync: %s", body)
	}
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin, sync on with the profile here: %s", body)
	}
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":false}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("sync off again: %s", body)
	}

	away := profile + "-away"
	if err := os.Rename(profile, away); err != nil {
		t.Fatal(err)
	}
	u := snapshot(g)
	body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("Thunderbird sync can't be switched on", "the mail folder this archive was made from", "isn't on this computer right now", "Nothing was changed."),
		assure.NoSideEffect(u.holds))
	// The folder is named as the archive recorded it (resolved, and in lower case where the
	// file system ignores case).
	if !strings.Contains(strings.ToLower(body), strings.ToLower(filepath.Base(store))) {
		t.Errorf("the refusal does not name the missing mail folder %s: %s", store, body)
	}

	// Already on: switching nothing on refuses nothing for the sync's sake — with the profile
	// back, and with it away again: an edit needs only its job file, never the archive's record
	// or its sources (design 3.3, R3C-16; MA-581's precondition lifted 2026-10-28).
	if err := os.Rename(away, profile); err != nil {
		t.Fatal(err)
	}
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("sync on with the profile back: %s", body)
	}
	if body := g.post(`{"interval":"daily","time":"02:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Errorf("an edit that keeps the sync on was refused: %s", body)
	}
	if err := os.Rename(profile, away); err != nil {
		t.Fatal(err)
	}
	if page, _ := getPage(t, g.h); !strings.Contains(page, `id="schedInterval"`) || !strings.Contains(page, `id="schedPresync"`) {
		t.Error("with the mail folder away, Settings no longer offers the installed run-job's edit form")
	}
	if body := g.post(`{"interval":"weekly","time":"04:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Errorf("an edit that keeps the sync on, with the mail folder away, was refused: %s", body)
	}
	if err := os.Rename(away, profile); err != nil {
		t.Fatal(err)
	}
	if j, err := job.Read(g.jobPath); err != nil || j.ThunderbirdSync == nil {
		t.Errorf("the sync did not stay on: %v", err)
	}
}

// covers: MA-860, R37
// A sync box the page did not show is never posted as off (C-10): the page's schedPost sends
// presync only when the box is on the page, and the server reads an absent presync as
// unchanged. Before, a hidden box was posted as false and the sync was switched off silently.
func TestAHiddenSyncBoxIsNeverPostedAsOff(t *testing.T) {
	i := strings.Index(dashboardJS, "function schedPost(")
	if i < 0 {
		t.Fatal("schedPost not found in the page script")
	}
	fn := dashboardJS[i:]
	if j := strings.Index(fn, "\n}\n"); j > 0 {
		fn = fn[:j]
	}
	if strings.Contains(fn, "presync:ps?ps.checked:false") || !strings.Contains(fn, "if(ps)body.presync=ps.checked") {
		t.Errorf("schedPost posts a hidden sync box as off:\n%s", fn)
	}

	g := newLocalRig(t, tbProfileArchive(t), "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create with sync: %s", body)
	}
	if body := g.post(`{"interval":"daily","time":"04:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("an edit with no sync box: %s", body)
	}
	if j, err := job.Read(g.jobPath); err != nil || j.ThunderbirdSync == nil {
		t.Errorf("an edit that did not post the sync switched it off (err %v)", err)
	}
}

// covers: MA-860, R37
// A job file that exists but can't be read is never overwritten (C-6): a file a newer version
// wrote, a damaged one, and a link are each refused naming the file, why, and the fix, with
// the file, the descriptor, the settings and the scheduler untouched. Positive twin first: a
// readable job file is edited.
func TestAnUnreadableJobFileIsNeverOverwritten(t *testing.T) {
	out := tbProfileArchive(t)
	g := newLocalRig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	if body := g.post(`{"interval":"daily","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin, an edit of a readable job file: %s", body)
	}

	cases := []struct {
		name, why string
		plant     func()
	}{
		{"newer version", "a newer version of MailArchive wrote it", func() {
			os.Remove(g.jobPath)
			if err := os.WriteFile(g.jobPath, []byte(`{"version":99,"name":"x","out":`+jsonQuote(out)+`,"mode":"incremental","future":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"damaged", "it is damaged", func() {
			os.Remove(g.jobPath)
			if err := os.WriteFile(g.jobPath, []byte(`{"version":1,"out":`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"link", "it is a link to another file", func() {
			os.Remove(g.jobPath)
			target := filepath.Join(t.TempDir(), "elsewhere.json")
			if err := job.Write(target, job.Job{Name: "x", Out: out, Mode: "incremental"}); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, g.jobPath); err != nil {
				t.Skipf("symlinks unsupported here: %v", err)
			}
		}},
	}
	for _, c := range cases {
		c.plant()
		u := snapshot(g)
		body := g.post(`{"interval":"weekly","time":"06:00","presync":false}`)
		assure.Refused(t, refusedCode(body), body,
			assure.Names("This archive's backup settings file "+g.jobPath+" can't be read", c.why, "Re-create the backup in the MailArchive wizard", "Nothing was changed."),
			assure.NoSideEffect(u.holds))
		if fi, err := os.Lstat(g.jobPath); err != nil || (c.name == "link") != (fi.Mode()&os.ModeSymlink != 0) {
			t.Errorf("%s: the job file was replaced", c.name)
		}
	}
}
