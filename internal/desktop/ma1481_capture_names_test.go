package desktop

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-1481, R18
// A scheduled Microsoft 365 capture refused at start (recordStartFailure: not set up) writes a failed
// record into its archive folder; when the record it replaces is plaintext, it keeps the Thunderbird
// folders' names (the 1.7.5 re-verify, round 4's RV4-01, the reviewer's test), as every record a run
// writes does (RV3-01). An encrypted record is replaced as before, with no names. Positive twin first:
// the planted record names the folder.
func TestACaptureRefusedAtStartKeepsTheFolderNames(t *testing.T) {
	out := t.TempDir()
	names := []state.TBName{{StoreID: "00112233445566778899aabbccddeeff", Name: "qzb@qzg.example"}}
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunFailed, Started: time.Now().UTC(), Error: "qz stopped", ThunderbirdNames: names}); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ := state.ReadLastRun(out); state.TBNameOf(lr, names[0].StoreID) != names[0].Name {
		t.Fatal("positive twin: the planted record names the folder")
	}
	recordStartFailure(out, errors.New(notConfiguredMsg))
	lr, _, _ := state.ReadLastRun(out)
	if lr.Status != state.RunFailed || lr.Error != notConfiguredMsg || state.TBNameOf(lr, names[0].StoreID) != names[0].Name {
		t.Errorf("recordStartFailure must write its failure and keep the folders' names: %q %q %+v", lr.Status, lr.Error, lr.ThunderbirdNames)
	}

	enc := t.TempDir()
	if err := state.WriteLastRun(enc, state.LastRun{Status: state.RunOK, Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	recordStartFailure(enc, errors.New(notConfiguredMsg))
	if b, _ := os.ReadFile(filepath.Join(enc, state.LastRunName)); bytes.Contains(b, []byte("thunderbird_names")) {
		t.Errorf("a record that replaced an encrypted one has names:\n%s", b)
	}
}

// covers: MA-1481, R12, R18
// The end-to-end form (the reviewer's other RV4-01 test, on the real writer): one archive folder
// backed up by Thunderbird and by a Microsoft 365 capture whose setup is gone. Three accounts on one
// mail server; the archive holds the first two; the third has always kept its mail off the profile;
// the second is moved. A Thunderbird backup stops (positive twin); the capture is refused at start
// and writes its record; the next Thunderbird backup still stops, by the record's name, never skipped
// quietly after one stop.
func TestAMovedAccountStillStopsAfterACaptureRefusedAtStart(t *testing.T) {
	_, _, profile := goneHome(t)
	a := filepath.Join(profile, "ImapMail", "imap.qzg.example")
	b := filepath.Join(profile, "ImapMail", "imap.qzg-1.example")
	third := filepath.Join(t.TempDir(), "QZUSB", "QZGmail3")
	for _, d := range []string{a, b, third} {
		mailIn(t, d)
	}
	sa := qzServer{n: 1, name: "qza@qzg.example", rel: "ImapMail/imap.qzg.example"}
	sb := qzServer{n: 2, name: "qzb@qzg.example", rel: "ImapMail/imap.qzg-1.example"}
	sc := qzServer{n: 3, name: "qzc@qzg.example", dir: third}
	qzPrefs(t, profile, sa, sb, sc)
	out := archiveOf(t, true, a, b)
	// The record names the folders, as an ok run that assessed them leaves them (RV3-01).
	lr, _, _ := state.ReadLastRun(out)
	lr.Thunderbird, lr.ThunderbirdNames = nil, []state.TBName{{StoreID: autoplace.StoreID(profile, a), Name: sa.name}, {StoreID: autoplace.StoreID(profile, b), Name: sb.name}}
	if err := state.WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	away := filepath.Join(t.TempDir(), "QZUSB", "QZGmail2")
	mailIn(t, away)
	sb.rel, sb.dir = "", away
	qzPrefs(t, profile, sa, sb, sc)
	run := func() (app.Result, error) {
		return app.Run(context.Background(), app.Options{Inputs: []string{a, b}, Auto: true, Out: out, Mode: export.Incremental, Unattended: true}, log.New(io.Discard, "", 0), nil)
	}
	if _, err := run(); err == nil {
		t.Fatal("positive twin: the first Thunderbird backup after the move stops")
	}
	recordStartFailure(out, errors.New(notConfiguredMsg))
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	before, _ := os.ReadFile(mpath)
	res, err := run()
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("input "+b+" does not exist", away, "Thunderbird still has the account that used it"),
		assure.Forbid(sb.name),
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(mpath)
			return bytes.Equal(before, after) && res.Stats.Exported == 0
		}))
}
