package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/job"
)

// planEnv is the production destination checks with this host's own drive called
// local and not removable, so a test's verdict does not depend on where the
// temporary folder is.
func planEnv() *CopyExportEnv {
	return &CopyExportEnv{
		removableSeam: func(string) bool { return false },
		view:          func(string) ceView { return ceView{Kind: viewLocal} },
	}
}

var planDay = time.Date(2026, 10, 27, 15, 0, 0, 0, time.Local)

// covers: MA-815, R37
// The plan of an unencrypted copy (phase 3 §10, X14) is made without the key and
// writes nothing: it names the new folder exactly — <archive>-unencrypted-<date>, the
// first free -N when that is taken — gives an upper bound on its size above the
// sealed files' and the free space, warns when the space may not do, and asks once
// for a folder on this computer's own drive. It is bound to the record as planned:
// rechecked at once it holds; after a backup, or with the new name taken meanwhile,
// it is stale (ErrDecryptStale).
func TestTheCopysPlanNamesItsFolderAndWritesNothing(t *testing.T) {
	out := sealedArchive(t)
	parent := tmpDir(t)
	beforeOut, beforeParent := snapshotTree(t, out), snapshotTree(t, parent)
	e := planEnv()
	p, err := e.PlanDecryptCopy(out, parent, nil, nil, planDay)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Base(out) + "-unencrypted-2026-10-27"
	if p.Name != want || p.Dest != filepath.Join(parent, want) {
		t.Fatalf("the plan names %q at %q, want %q", p.Name, p.Dest, want)
	}
	if sealed := sealedSize(out); sealed == 0 || p.NeedBytes <= sealed {
		t.Fatalf("the size bound %d is not above the sealed files' %d", p.NeedBytes, sealed)
	}
	if !p.FreeKnown || p.FreeBytes == 0 {
		t.Fatalf("the free space was not read: %d %v", p.FreeBytes, p.FreeKnown)
	}
	if p.AskTwice() || len(p.Leaves) != 0 {
		t.Fatalf("a folder on this computer's own drive asks twice: %v", p.Leaves)
	}
	if !sameTree(beforeOut, snapshotTree(t, out)) || !sameTree(beforeParent, snapshotTree(t, parent)) {
		t.Fatal("planning wrote something")
	}
	if err := p.Recheck(e, nil, nil); err != nil {
		t.Fatalf("a plan rechecked at once: %v", err)
	}

	os.Mkdir(p.Dest, 0o700)
	if err := p.Recheck(e, nil, nil); !errors.Is(err, ErrDecryptStale) {
		t.Fatalf("the new name taken meanwhile: want ErrDecryptStale, got %v", err)
	}
	p2, err := e.PlanDecryptCopy(out, parent, nil, nil, planDay)
	if err != nil || p2.Name != want+"-2" {
		t.Fatalf("with the name taken the plan names %q (%v), want %q", p2.Name, err, want+"-2")
	}

	mirror := filepath.Join(out, container.MirrorName)
	raw, _ := os.ReadFile(mirror)
	os.WriteFile(mirror, append(raw, ' '), 0o600)
	if err := p2.Recheck(e, nil, nil); !errors.Is(err, ErrDecryptStale) {
		t.Fatalf("the record changed since the plan: want ErrDecryptStale, got %v", err)
	}

	e.freeDest = func(string) (uint64, error) { return 1024, nil }
	p3, err := e.PlanDecryptCopy(out, parent, nil, nil, planDay)
	if err != nil || len(p3.Warnings) != 1 || !strings.Contains(p3.Warnings[0], "may not have room") {
		t.Fatalf("too little room: want one warning, got %v (%v)", p3.Warnings, err)
	}
}

// covers: MA-815, R37
// A destination that may leave this computer asks twice (K5), each reason named in
// the plan: a synced folder, a removable or external drive, a network share.
func TestTheCopysPlanAsksTwiceWhereTheCopyMayLeave(t *testing.T) {
	out := sealedArchive(t)
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveCommercial", "")
	t.Setenv("OneDriveConsumer", "")
	parent := tmpDir(t)
	synced := filepath.Join(parent, "Dropbox")
	os.Mkdir(synced, 0o700)
	for _, c := range []struct {
		name   string
		parent string
		env    func(e *CopyExportEnv)
		says   string
	}{
		{"synced", synced, func(*CopyExportEnv) {}, "synced to Dropbox"},
		{"removable", parent, func(e *CopyExportEnv) { e.removableSeam = func(string) bool { return true } }, "removable or external drive"},
		{"share", parent, func(e *CopyExportEnv) {
			e.view = func(p string) ceView {
				if p == parent {
					return ceView{Kind: viewShare, Server: "nas.example", Mount: "//nas.example/mail"}
				}
				return ceView{Kind: viewLocal}
			}
		}, "network share"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := planEnv()
			c.env(e)
			p, err := e.PlanDecryptCopy(out, c.parent, nil, nil, planDay)
			if err != nil {
				t.Fatal(err)
			}
			if !p.AskTwice() || len(p.Leaves) != 1 || !strings.Contains(p.Leaves[0], c.says) || !strings.Contains(p.Leaves[0], c.parent) {
				t.Fatalf("want one reason saying %q about %s, got %v", c.says, c.parent, p.Leaves)
			}
		})
	}
}

// covers: MA-815, R37
// The copy's destination refusals, each in the copy's words, naming the folder, with
// nothing written: no folder chosen, a path that is not full, a folder that does not
// exist or is a file, the archive itself or a folder inside it, inside another
// archive this computer knows or one found above the folder, inside an encrypted
// archive, inside MailArchive's settings, record, sign-in or cache folders or a
// front end's own settings folder; and a source that is not an encrypted archive
// MailArchive can open. The positive twin is the plan above.
func TestTheCopysPlanRefusesEachUnsafeDestination(t *testing.T) {
	home := tmpDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "LocalAppData"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	out := sealedArchive(t)
	inner := filepath.Join(out, "inner")
	os.Mkdir(inner, 0o700)
	other := plainArchive(t)
	otherInner := filepath.Join(other, "sub")
	os.Mkdir(otherInner, 0o700)
	enc2 := sealedArchive(t)
	enc2Inner := filepath.Join(enc2, "sub")
	os.Mkdir(enc2Inner, 0o700)
	file := filepath.Join(tmpDir(t), "a-file")
	os.WriteFile(file, []byte("x"), 0o600)
	cfg, _ := job.Dir()
	inCfg := filepath.Join(cfg, "copies")
	os.MkdirAll(inCfg, 0o700)
	scratch, _ := job.ScratchDir()
	inCache := filepath.Join(filepath.Dir(scratch), "copies")
	os.MkdirAll(inCache, 0o700)
	front := tmpDir(t)
	inFront := filepath.Join(front, "copies")
	os.Mkdir(inFront, 0o700)
	plain := plainArchive(t)
	future := tmpDir(t) // an archive location named but not backed up into yet: no manifest
	inFuture := filepath.Join(future, "copies")
	os.Mkdir(inFuture, 0o700)

	for _, c := range []struct {
		name, src, parent string
		archives          []string
		id                string
		names             string
		says              string
	}{
		{"none chosen", out, "", nil, "R-e", "", "No folder is chosen"},
		{"not a full path", out, "copies/here", nil, "R-e", "copies/here", "isn't a full path"},
		{"missing", out, filepath.Join(home, "nowhere"), nil, "R-e", filepath.Join(home, "nowhere"), "doesn't exist"},
		{"a file", out, file, nil, "R-e", file, "is a file, not a folder"},
		{"the archive itself", out, out, nil, "R-enc", out, "is an encrypted archive"},
		{"inside the archive", out, inner, nil, "R-enc", inner, "never made inside one"},
		{"inside another listed archive", out, otherInner, []string{other}, "R-f", other, "inside an archive"},
		{"inside an archive found above", out, otherInner, nil, "R-f", other, "inside an archive"},
		{"inside a listed archive not backed up yet", out, inFuture, []string{future}, "R-f", future, "inside an archive"},
		{"inside another encrypted archive", out, enc2Inner, nil, "R-enc", enc2Inner, "never made inside one"},
		{"inside the settings folder", out, inCfg, nil, "R-set", inCfg, "uninstalling MailArchive removes it"},
		{"inside the cache folder", out, inCache, nil, "R-set", inCache, "uninstalling MailArchive removes it"},
		{"inside a front end's settings", out, inFront, nil, "R-set", inFront, "uninstalling MailArchive removes it"},
		{"a plaintext source", plain, home, nil, "R-src", plain, "is not an encrypted archive"},
		{"a folder inside the source", inner, home, nil, "R-src", inner, "is not an encrypted archive"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := snapshotTree(t, home)
			_, err := planEnv().PlanDecryptCopy(c.src, c.parent, c.archives, []string{front}, planDay)
			var r *CopyRefusal
			if !errors.As(err, &r) || r.ID != c.id {
				t.Fatalf("want refusal %s, got %v", c.id, err)
			}
			assure.Refused(t, 1, r.Message, assure.Code(r.ExitCode()), assure.Names(c.names, c.says),
				assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, home)) }))
		})
	}
}
