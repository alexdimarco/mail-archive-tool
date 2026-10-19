package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

const (
	testMe     = "S-1-5-21-111-222-333-1001"
	testUsers  = "S-1-5-32-545"
	testBobSID = "S-1-5-21-111-222-333-2002"
)

// ownerOnly is the DACL of a destination only its owner, SYSTEM and Administrators can
// use, every entry inheritable (OI and CI).
func ownerOnly() []ceACE {
	return []ceACE{
		{Allow: true, SID: testMe, Who: `PC\alex`, Read: true, OI: true, CI: true},
		{Allow: true, SID: sidSystem, Who: `NT AUTHORITY\SYSTEM`, Read: true, OI: true, CI: true},
		{Allow: true, SID: sidAdministrator, Who: `BUILTIN\Administrators`, Read: true, OI: true, CI: true},
	}
}

// covers: MA-532, R29, R12, S52
// The hand-over posture is computed over what a new child of the destination inherits,
// not over the destination's own access (the evaluation, on every OS, of a Windows
// descriptor and of a macOS ACL): an owner-only DACL reads "only you"; with an
// inherit-only (OI|CI|IO) ACE granting BUILTIN\Users read the line names Users, though the
// destination's own effective access is still owner-only (the mutation's own answer); an
// inheritable ACE for one other account names that account; a deny before an allow takes
// the read away; a descriptor that cannot be read is "couldn't check who can read this
// folder", never "only you"; on macOS an inheritable "everyone allow read" entry names
// everyone, and an inherit-only entry does not reach the destination itself.
func TestCopyExportPostureIsComputedOverWhatANewChildInherits(t *testing.T) {
	if p := windowsPosture(ownerOnly(), testMe, true); p.Kind != postureKindPrivate || p.Line != "only you can read this folder" {
		t.Fatalf("an owner-only destination: %+v", p)
	}
	usersIO := append(ownerOnly(), ceACE{Allow: true, SID: testUsers, Who: `BUILTIN\Users`, Read: true, OI: true, CI: true, IO: true})
	p := windowsPosture(usersIO, testMe, true)
	if p.Kind != postureKindReaders || strings.Join(p.Readers, ",") != `BUILTIN\Users` || p.Line != `BUILTIN\Users can read what you export here` {
		t.Errorf("an inherit-only ACE for BUILTIN\\Users: %+v", p)
	}
	if own := windowsRealized(usersIO, testMe, true); own.Kind != postureKindPrivate {
		t.Errorf("the destination's own effective access is %+v: an inherit-only entry must not apply to it (the check that reads 'only you' wrongly)", own)
	}
	bob := append(ownerOnly(), ceACE{Allow: true, SID: testBobSID, Who: `CONTOSO\bob`, Read: true, OI: true, CI: true})
	if p := windowsPosture(bob, testMe, true); strings.Join(p.Readers, ",") != `CONTOSO\bob` {
		t.Errorf("an inheritable ACE for one account: %+v", p)
	}
	both := append(usersIO, bob[3])
	if p := windowsPosture(both, testMe, true); p.Line != `BUILTIN\Users and CONTOSO\bob can read what you export here` {
		t.Errorf("two readers: %q", p.Line)
	}
	denied := append([]ceACE{{Allow: false, SID: testUsers, Who: `BUILTIN\Users`, Read: true, OI: true, CI: true}}, usersIO...)
	if p := windowsPosture(denied, testMe, true); p.Kind != postureKindPrivate {
		t.Errorf("a deny before the allow: %+v", p)
	}
	filesOnly := append(ownerOnly(), ceACE{Allow: true, SID: testBobSID, Who: `CONTOSO\bob`, Read: true, OI: true})
	if p := windowsPosture(filesOnly, testMe, true); strings.Join(p.Readers, ",") != `CONTOSO\bob` {
		t.Errorf("an ACE for files only still reaches the files below the root: %+v", p)
	}
	noRead := append(ownerOnly(), ceACE{Allow: true, SID: testUsers, Who: `BUILTIN\Users`, Read: false, OI: true, CI: true})
	if p := windowsPosture(noRead, testMe, true); p.Kind != postureKindPrivate {
		t.Errorf("an ACE that grants no read: %+v", p)
	}
	unknown := windowsPosture(nil, testMe, false)
	if unknown.Kind != postureKindUnknown || unknown.Line != "couldn't check who can read this folder" || strings.Contains(unknown.Line, "only you") {
		t.Errorf("an unreadable descriptor: %+v", unknown)
	}

	mac := []ceMacACE{{Allow: true, Who: "everyone", Read: true, FileInherit: true, DirInherit: true}}
	if p := macPosture(mac, "user 501", true); p.Kind != postureKindReaders || strings.Join(p.Readers, ",") != "everyone" {
		t.Errorf("macOS inheritable everyone: %+v", p)
	}
	if p := macPosture(nil, "user 501", true); p.Kind != postureKindPrivate {
		t.Errorf("macOS with no ACL: %+v", p)
	}
	if p := macRealized([]ceMacACE{{Allow: true, Who: "everyone", Read: true, FileInherit: true, Only: true}}, "user 501", true); p.Kind != postureKindPrivate {
		t.Errorf("a macOS inherit-only entry is not effective on the object itself: %+v", p)
	}
	if p := macPosture(nil, "user 501", false); p.Kind != postureKindUnknown {
		t.Errorf("macOS with an unreadable ACL: %+v", p)
	}
	// A posture that cannot tell is broader than one that could, a named reader not
	// named before is broader, the same reader is not.
	priv, users := newPosture(postureKindPrivate, nil), newPosture(postureKindReaders, []string{`BUILTIN\Users`})
	if !unknown.broaderThan(priv) || !users.broaderThan(priv) || users.broaderThan(users) || priv.broaderThan(users) {
		t.Error("broaderThan does not order the postures")
	}
}

// covers: MA-532, R29, R12, S52
// Start recomputes the posture and refuses with R-w2, writing nothing, when it is broader
// than the one the preview showed: a destination that gained an inheritable ACE for
// BUILTIN\Users, or one whose descriptor can no longer be read. The positive twin, the
// same posture as the preview, starts.
func TestCopyExportStartRefusesABroaderPosture(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	now := newPosture(postureKindPrivate, nil)
	e.posture = func(string) CopyPosture { return now }
	p := cePlan(t, e, src, ceMsgs(2))
	if p.Posture.Kind != postureKindPrivate {
		t.Fatalf("the preview's posture is %+v", p.Posture)
	}
	if res := mustRun(t, e, p, src); res.Counts.Exported != 2 {
		t.Fatalf("the positive twin: %+v", res.Counts)
	}

	for _, tc := range []struct {
		name   string
		later  CopyPosture
		names  []string
		forbid []string
	}{
		{"a new reader", newPosture(postureKindReaders, []string{`BUILTIN\Users`}), []string{`BUILTIN\Users`, "only you could", "Preview copy export again"}, nil},
		{"an unreadable descriptor", newPosture(postureKindUnknown, nil), []string{"couldn't check who can read this folder", "Preview copy export again"}, nil},
	} {
		now = newPosture(postureKindPrivate, nil)
		p := cePlan(t, e, src, ceMsgs(2))
		now = tc.later
		before, _ := os.ReadDir(p.Dest)
		records, _ := os.ReadDir(e.recordsDir())
		_, err := ceRunPlan(t, e, p, src)
		var ref *CopyRefusal
		if !errors.As(err, &ref) || ref.ID != "R-w2" {
			t.Fatalf("%s: Start did not refuse with R-w2: %v", tc.name, err)
		}
		assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(tc.names...), assure.Forbid(tc.forbid...),
			assure.NoSideEffect(func() bool {
				after, _ := os.ReadDir(p.Dest)
				recs, _ := os.ReadDir(e.recordsDir())
				return len(after) == len(before) && len(recs) == len(records)
			}))
	}
}

// covers: MA-532, R29, R12, S52
// The realized check: an ACE that appears between Start's check and W4 (a fault hook) is
// seen on the created root and marker, which are read again before the first message: the
// run stops before W5 with R-w2, nothing is exported (the source is never asked for a
// byte), the marker is rewritten STOPPED, and the realized posture is in the run record. On
// a share whose preview said its prediction may differ, the realized line is confirmed
// instead, and declining stops the run the same way.
func TestCopyExportRealizedPostureStopsTheRunBeforeTheFirstMessage(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	e.posture = func(string) CopyPosture { return newPosture(postureKindPrivate, nil) }
	broader := false
	e.realized = func(path string, isDir bool) CopyPosture {
		if broader {
			return newPosture(postureKindReaders, []string{`BUILTIN\Users`})
		}
		return newPosture(postureKindPrivate, nil)
	}
	e.faults = &copyFaults{hook: func(step string) {
		if step == "W3" {
			broader = true // the ACE appears after Start's check, before the root is read again
		}
	}}
	p := cePlan(t, e, src, ceMsgs(3))
	res := mustRun(t, e, p, src)
	if res.Stop == nil || res.Stop.RID != "R-w2" || res.Outcome != CopyOutcomeFailed || res.Counts.Exported != 0 {
		t.Fatalf("a realized posture broader than the line ended as %s (%+v) %+v", res.Label, res.Stop, res.Counts)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names(`BUILTIN\Users`, "nothing was exported"))
	if len(src.fetches) != 0 {
		t.Errorf("the source was asked for %v: nothing may be exported before the check", src.fetches)
	}
	if m := readText(t, res.Root, copyMarkerName); !strings.Contains(m, "STOPPED") {
		t.Errorf("the marker is %q, want STOPPED", m)
	}
	if sum := mustSummary(t, e, res.RunID); sum.Realized == nil || sum.Realized.Kind != postureKindReaders || len(sum.Realized.Readers) != 1 {
		t.Errorf("the realized posture is not in the run record: %+v", sum.Realized)
	}

	// A share: the preview said its prediction may differ.
	e.faults = nil
	broader = false
	e.posture = func(string) CopyPosture { pp := newPosture(postureKindPrivate, nil); pp.Predicted = true; return pp }
	broader = true
	p = cePlan(t, e, src, ceMsgs(2))
	run := e.NewCopyRun(p, src)
	declined := 0
	run.ConfirmPosture = func(CopyPosture) bool { declined++; return false }
	res2, err := run.Run(context.Background())
	if err != nil || res2.Stop == nil || res2.Stop.RID != "R-w2" || declined != 1 {
		t.Fatalf("declining the realized line: %+v (%v), asked %d times", res2, err, declined)
	}
	p = cePlan(t, e, src, ceMsgs(2))
	run = e.NewCopyRun(p, src)
	run.ConfirmPosture = func(CopyPosture) bool { return true }
	if res3, err := run.Run(context.Background()); err != nil || res3.Counts.Exported != 2 {
		t.Errorf("accepting the realized line: %+v (%v)", res3, err)
	}
}

// covers: MA-532, R29, R12, S52
// On a filesystem that cannot restrict access (FAT, exFAT, a share) the line says so, from
// the filesystem's type, before any descriptor is read; and the real POSIX read of a
// directory this run created reports it private at 0700.
func TestCopyExportPostureOnFilesystemsThatCannotRestrictAccess(t *testing.T) {
	e := newCEEnv(t)
	for _, fsType := range []string{"vfat", "exfat", "msdos", "smbfs", "cifs"} {
		p := e.destinationPosture(t.TempDir(), fsType)
		if p.Kind != postureKindUnenforceable || p.Line != "permissions can't be restricted on this drive" {
			t.Errorf("%s: %+v", fsType, p)
		}
	}
	if p := e.destinationPosture(t.TempDir(), "smbfs"); !p.Predicted {
		t.Error("a share's line is a prediction")
	}
	if p := e.destinationPosture(t.TempDir(), "apfs"); p.Kind != postureKindPrivate && p.Kind != postureKindReaders {
		t.Errorf("a local filesystem's line is %+v", p)
	}
	dir := filepath.Join(t.TempDir(), "made")
	os.Mkdir(dir, 0o700)
	if p := osRealizedPosture(dir, true); p.Kind != postureKindPrivate {
		t.Errorf("a 0700 directory this process made reads %+v", p)
	}
}
