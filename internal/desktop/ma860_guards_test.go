package desktop

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/seal"
)

// covers: MA-860, R37
// A Save on a paused (sealed) archive is refused (C-11): Pause removed its schedule and kept the
// descriptor, and Save never re-creates it behind the seal — scheduler, descriptor, job file and
// settings untouched. Before, nothing checked the seal. Positive twin first: unsealed, it saves.
func TestASaveOnAPausedArchiveIsRefused(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin: %s", body)
	}
	g.recordLast()
	if err := seal.Seal(out, "testing"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { seal.Unseal(out) })
	for _, req := range []string{`{"interval":"daily","time":"04:00"}`, `{}`} {
		u := g.snapshot()
		body := g.post(req)
		assure.Refused(t, refusedCode(body), body, assure.Names(pausedRefusal), assure.NoSideEffect(u.holds))
	}
}

// covers: MA-860, R37
// One Save, one archive (C-12): both requests name the archive folder the page was drawn for, and
// one drawn for another archive than the current one is refused with nothing changed — the
// schedule untouched, the settings file as it was. The page carries the folder and its script
// sends it on both requests. Positive twin first: drawn for this archive, each succeeds.
func TestAPageDrawnForAnotherArchiveChangesNothing(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	other := filepath.Join(t.TempDir(), "other")
	g := newA3Rig(t, out, "darwin")
	if page := g.page(); !strings.Contains(page, `<meta name="drawn-for" content="`+out+`">`) {
		t.Error("the page does not carry the folder it was drawn for")
	}
	for _, want := range []string{"drawnFor:drawnFor};\n  if(ps)body.presync", "backup:backup||'',drawnFor:drawnFor}"} {
		if !strings.Contains(dashboardJS, want) {
			t.Errorf("the page script does not send drawnFor (%q)", want)
		}
	}

	if body := g.post(`{"interval":"weekly","time":"03:00","drawnFor":` + jsonQuote(out) + `}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin, a schedule request drawn for this archive: %s", body)
	}
	u := g.snapshot()
	body := g.post(`{"interval":"daily","time":"04:00","drawnFor":` + jsonQuote(other) + `}`)
	assure.Refused(t, refusedCode(body), body, assure.Names(staleRefusal), assure.NoSideEffect(u.holds))

	save := func(drawn string) string {
		return postJSON(g.h, "/api/settings", g.csrf, `{"out":`+jsonQuote(out)+`,"keepRaw":true,"drawnFor":`+jsonQuote(drawn)+`}`).Body.String()
	}
	if body := save(out); !strings.Contains(body, `"ok":true`) || !LoadSettings(g.settings).KeepRaw {
		t.Fatalf("the positive twin, a settings save drawn for this archive: %s", body)
	}
	before := fileBytes(g.settings)
	body = postJSON(g.h, "/api/settings", g.csrf, `{"out":`+jsonQuote(other)+`,"keepRaw":false,"drawnFor":`+jsonQuote(other)+`}`).Body.String()
	assure.Refused(t, refusedCode(body), body, assure.Names(staleRefusal),
		assure.NoSideEffect(func() bool { return bytes.Equal(before, fileBytes(g.settings)) }))
}

// covers: MA-860, R37
// A Save while a backup holds the archive's lock is refused (C-18): reloading the scheduler entry
// could stop the run. Nothing is changed. Positive twin first: with the lock free, it saves.
func TestASaveWhileABackupRunsIsRefused(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin: %s", body)
	}
	l, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	u := g.snapshot()
	body := g.post(`{"interval":"daily","time":"04:00"}`)
	assure.Refused(t, refusedCode(body), body, assure.Names(runningRefusal), assure.NoSideEffect(u.holds))
}
