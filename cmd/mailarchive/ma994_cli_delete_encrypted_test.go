package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-994, R36, R37
// delete-archive deletes an encrypted archive without its password, and this computer forgets
// its record and its remembered key (design-archive-delete §3.3, §6, §5.3 step 8): the command
// line has no page, so nothing is "locked on this page".
func TestDeleteArchiveForgetsAnEncryptedArchivesRecordAndKey(t *testing.T) {
	e := newCLIDel(t)
	store := flowRecord(t)
	out := filepath.Join(t.TempDir(), "Sealed archive")
	// The password twice, remember the key (Enter: yes), program copies (Enter), the key shown.
	mk := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "", "", keyAnswer}}
	u, err := createEncrypted(mk, out, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("make the archive: %v\n%s", err, mk.transcript())
	}
	u.Destroy()
	e.list(out)
	id := recordedAt(t, out)
	// Positive twin: recorded here, its key remembered.
	assure.Reached(t, id, "this computer's record of the archive")
	if _, err := store.Get("archive:" + id); err != nil {
		t.Fatalf("the key is not remembered before the delete: %v", err)
	}

	a := &scripted{isAttended: true, answers: []string{"Sealed archive"}}
	var res bytes.Buffer
	if err := deleteArchive(a, out, "", "", &res); err != nil {
		t.Fatalf("delete-archive of an encrypted archive: %v\n%s\n%s", err, a.transcript(), res.String())
	}
	for _, p := range a.prompts {
		if strings.Contains(strings.ToLower(p), "password") || strings.Contains(strings.ToLower(p), "recovery key") {
			t.Fatalf("a password was asked: %q", p)
		}
	}
	// The last copy this computer recorded, as the real keyring's forget plan says (MA-1010).
	if !strings.Contains(strings.Join(a.said, "\n"), "  If the folder is removed, this computer will forget this archive's key: this is the last copy of it this computer recorded. Any other copy of this archive will then need its password or recovery key to open.") {
		t.Fatalf("the plan does not say the key is forgotten with the folder:\n%s", a.transcript())
	}
	r := res.String()
	for _, want := range []string{"Deleted.", "The folder is gone.", "This computer forgot the archive's key", "Removed from Your archives", "Messages: not known: encrypted, key not open"} {
		if !strings.Contains(r, want) {
			t.Fatalf("the result does not say %q:\n%s", want, r)
		}
	}
	if strings.Contains(r, "locked on this page") {
		t.Fatalf("the command line says the archive was locked on a page:\n%s", r)
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatalf("the folder is still there: %v", err)
	}
	if recordedAt(t, out) != "" {
		t.Fatal("this computer still records the archive")
	}
	if _, err := store.Get("archive:" + id); err == nil {
		t.Fatal("this computer still remembers the archive's key")
	}
	if e.listed(out) {
		t.Fatal("the row is still in Your archives")
	}

	// Stopped part way (a Desktop found before step 4): still nothing "locked on this page".
	out2 := filepath.Join(t.TempDir(), "Second sealed")
	mk2 := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", "", keyAnswer}}
	u2, err := createEncrypted(mk2, out2, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("make the second archive: %v\n%s", err, mk2.transcript())
	}
	u2.Destroy()
	e.list(out2)
	var release func()
	beforeDeleteStep = func(step int) {
		if step == 4 {
			release = e.desktopRunning("")
		}
	}
	res.Reset()
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Second sealed"}}, out2, "", "", &res)
	beforeDeleteStep = func(int) {}
	if exitCode(err) != 3 || !strings.Contains(res.String(), "The delete stopped before step 4") {
		t.Fatalf("the stop before step 4: exit %d (%v)\n%s", exitCode(err), err, res.String())
	}
	if strings.Contains(res.String(), "locked on this page") {
		t.Fatalf("the command line says the archive was locked on a page:\n%s", res.String())
	}
	release()
}
