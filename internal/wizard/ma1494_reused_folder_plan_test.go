package wizard

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
)

// covers: MA-1494, MA-1495, MA-1509, R12
// RV-01 (the re-verify of rounds A and B, MEDIUM): Thunderbird gives a removed account's folder to a
// new account, inside a set. qzalice's archive in the set's parent keeps her account's identity; she is
// removed with her mail data and qzcarol, added on the same mail server, gets the same folder, so every
// run of alice's archive stops as reused (MA-1509). The set's re-run never plans carol's mailbox into
// alice's archive: carol gets an archive of her own (made and exported), and alice's is listed "kept as
// it is, since another account uses its folder now; click Pause (make read-only) on its row under Your
// archives in MailArchive Desktop" and is never written into. After the advised Pause it is never taken
// as carol's paused archive either: carol's own archive is brought up to date, or, when it is not there,
// made again; alice's line then gives no Pause advice. Positive twin first: while alice's account owns
// her folder, the plan brings her archive up to date.
func TestAFolderAnotherAccountUsesNowGetsItsOwnArchiveInASet(t *testing.T) {
	ownSettings(t)
	const host = "imap.qzr.example"
	profile, prefs := movedAwayProfile(t, host)
	store := filepath.Join(profile, "ImapMail", host)
	prefs([3]string{host, host, "qzalice@qzr.example"})
	parent := filepath.Join(t.TempDir(), "Mail archives")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	alice := filepath.Join(parent, "qzalice@qzr.example")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{store}, Out: alice, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(alice, 0o700); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ := state.ReadLastRun(alice); len(lr.ThunderbirdIDs) != 1 {
		t.Fatalf("alice's archive's record must keep her folder's account identity: %+v", lr.ThunderbirdIDs)
	}

	// Positive twin: alice's account still owns her folder.
	aliceRows := []setRow{{path: store, name: "qzalice@qzr.example", shown: "qzalice@qzr.example", make: true}}
	if p, err := planSet(parent, aliceRows, runtime.GOOS); err != nil || !reflect.DeepEqual(outcomesOf(p), []string{"qzalice@qzr.example: continue qzalice@qzr.example"}) {
		t.Fatalf("positive twin: the archive of a folder its account still uses is brought up to date: %v %q", err, outcomesOf(p))
	}

	// Alice removed with her mail data; carol added on the same server gets the same folder. A run of
	// alice's archive now stops as reused (MA-1509).
	prefs([3]string{host, host, "qzcarol@qzr.example"})
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{store}, Out: alice, Mode: export.Incremental, Unattended: true}, log.New(io.Discard, "", 0), nil); err == nil {
		t.Fatal("a run of alice's archive must stop as reused")
	}
	if lr, _, _ := state.ReadLastRun(alice); lr.SourceReason != state.SourceReasonReused {
		t.Fatalf("alice's archive's stop: %q", lr.SourceReason)
	}
	snapshot := func() []string {
		out := treeOf(t, alice)
		for _, f := range []string{".mailarchive-manifest.json", state.LastRunName} {
			b, _ := os.ReadFile(filepath.Join(alice, f))
			out = append(out, f+"="+string(b))
		}
		return out
	}
	before := snapshot()

	// The set's re-run, through every dialog seam: carol gets her own archive; alice's is kept.
	rows := []setRow{{path: store, name: "qzcarol@qzr.example", shown: "qzcarol@qzr.example", make: true}}
	carol := filepath.Join(parent, "qzcarol@qzr.example")
	(&setScheduler{}).install(t)
	s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	made, err := makeSet(rows, runtime.GOOS)
	plan := ""
	if len(s.questions) > 0 {
		plan = s.questions[0]
	}
	code := 0
	if err == nil && len(made) == 1 && made[0].New && made[0].Folder == carol && made[0].Outcome == MadeFinished && holdsOnly(carol, rows[0]) {
		code = 1
	}
	assure.Refused(t, code, plan, assure.Code(1),
		assure.Names("MailArchive will make 1 archive in "+parent+":\n\nqzcarol@qzr.example",
			"qzalice@qzr.example: kept as it is, since another account uses its folder now; click Pause (make read-only) on its row under Your archives in MailArchive Desktop."),
		assure.Forbid("Paused, so qzalice@qzr.example", "qzalice@qzr.example (already in this folder)"),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(snapshot(), before) }))

	// The advised Pause: alice's archive is never carol's paused archive. Carol's own is brought up to
	// date; alice's line gives no Pause advice.
	if err := seal.Seal(alice, "paused by the person"); err != nil {
		t.Fatal(err)
	}
	pausedLine := "qzalice@qzr.example: kept as it is, paused, since another account uses its folder now."
	p, err := planSet(parent, rows, runtime.GOOS)
	text := setPlanText(p)
	if want := []string{"qzcarol@qzr.example: continue qzcarol@qzr.example reused qzalice@qzr.example paused=true"}; err != nil || !reflect.DeepEqual(outcomesOf(p), want) ||
		!strings.Contains(text, pausedLine) || strings.Contains(text, setReusedKept) || strings.Contains(text, "Paused, so qzalice@qzr.example") {
		t.Errorf("after the Pause, carol's own archive is brought up to date and alice's kept: %v %q\n%s", err, outcomesOf(p), text)
	}
	// The re-verifier's sequence exactly: alice's archive paused and carol's not there (step 6).
	if err := os.RemoveAll(carol); err != nil {
		t.Fatal(err)
	}
	p, err = planSet(parent, rows, runtime.GOOS)
	text = setPlanText(p)
	if want := []string{"qzcarol@qzr.example: new qzcarol@qzr.example reused qzalice@qzr.example paused=true"}; err != nil || !reflect.DeepEqual(outcomesOf(p), want) ||
		!strings.Contains(text, "MailArchive will make 1 archive in "+parent+":\n\nqzcarol@qzr.example") || !strings.Contains(text, pausedLine) {
		t.Errorf("after the Pause, carol gets an archive of her own: %v %q\n%s", err, outcomesOf(p), text)
	}
}
