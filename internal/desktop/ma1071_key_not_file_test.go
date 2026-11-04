package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
)

// The recovery key and the recovery file told apart (MA-1071; the owner's report of 2026-11-03:
// "saving the recovery key gives a complex file - when asked to place it in the box it is a complex
// code file - no idea how to place it"). On the Windows VM the person saved the recovery file, opened
// it (JSON) and pasted it where the recovery key is typed back, and was told only "That is not a
// recovery key".

// typo changes the recovery key's first letter to another letter a recovery key may hold.
func typo(key string) string {
	if key[0] == 'A' {
		return "B" + key[1:]
	}
	return "A" + key[1:]
}

// covers: MA-1071, R37
// A new recovery key's recovery file: its folder window says what the file is and that it is not the
// recovery key; the file holds neither the recovery key nor the password; the line saying it was saved
// says it is opened by MailArchive, never typed, and does not hold the key. Typed back where the key
// is asked, its contents (whole, or one line of it) are told as the recovery file's, quoting none of
// it, and the key still waits: typed right, it takes effect. Positive twin first: a typing mistake in
// the key is still told as one.
func TestTheRecoveryFilesContentsTypedBackAreToldPlainly(t *testing.T) {
	out := backedUpEncrypted(t)
	saveDir := t.TempDir()
	var titles []string
	orig := pickFolder
	pickFolder = func(_ context.Context, title string) (string, error) {
		titles = append(titles, title)
		return saveDir, nil
	}
	t.Cleanup(func() { pickFolder = orig })
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, m := e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
	token, key := m["token"].(string), m["recoveryKey"].(string)
	if m["ok"] != true || token == "" || key == "" {
		t.Fatalf("prepared: %v", m)
	}
	_, m = e.post(t, "api/encryption/new-recovery-key/save-file", map[string]any{"token": token})
	path, _ := m["path"].(string)
	data, err := os.ReadFile(path)
	if m["ok"] != true || err != nil {
		t.Fatalf("saved: %v %v", m, err)
	}
	if len(titles) != 1 || titles[0] != recoveryFilePickTitle {
		t.Errorf("the folder window's title: %q, want %q", titles, recoveryFilePickTitle)
	}
	if m["note"] != recoveryFileSavedNote {
		t.Errorf("the saved line's words: %q, want %q", m["note"], recoveryFileSavedNote)
	}
	file := string(data)
	bare := strings.ReplaceAll(key, "-", "")
	if strings.Contains(file, key) || strings.Contains(strings.ToUpper(file), bare) || strings.Contains(file, keysPassword) {
		t.Fatal("the recovery file holds the recovery key or the password")
	}
	// Positive twin: a typing mistake is told as one.
	if _, m := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": typo(key)}); m["ok"] != false || m["error"] != keyring.RecoveryKeyRetry(vault.ErrRecoveryCheck) {
		t.Fatalf("a typing mistake: %v", m)
	}
	var kindLine string
	for _, l := range strings.Split(file, "\n") {
		if strings.Contains(l, `"kind"`) {
			kindLine = l
			break
		}
	}
	for what, typed := range map[string]string{"the whole file": file, "one line of it": kindLine} {
		_, m := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": typed})
		if m["ok"] != false || m["error"] != recoveryFileTypedBack {
			t.Errorf("%s typed back: %v, want %q", what, m, recoveryFileTypedBack)
		}
		if s := toText(m); strings.Contains(s, "mailarchive-recovery-file") || strings.Contains(s, "argon2id") {
			t.Errorf("%s typed back is quoted: %v", what, m)
		}
	}
	_, m = e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": key})
	u, err := keyring.Open(out, keyring.RecoveryKey(key), keyring.OpenOptions{})
	if err == nil {
		u.Destroy()
	}
	assure.Reached(t, m["ok"] == true && err == nil, "the key still waited and took effect when typed right")
}

// covers: MA-1071, R37
// Put back the key file: the recovery file's contents typed in the recovery key box are told as the
// recovery file's, with where the file is chosen instead, before any window opens, and nothing is
// written. Positive twin first: the archive's real recovery key opens the recovery file's window and
// prepares the key file (cancelled here).
func TestTheRecoveryFilesContentsInTheRestoreKeyBoxAreToldPlainly(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "Enc")
	p, err := keyring.Create(out, keysPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rk := p.RecoveryKey()
	rf, err := p.RecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	backUpInto(t, out, u)
	u.Destroy()
	rfPath := filepath.Join(t.TempDir(), "recovery.txt")
	if err := os.WriteFile(rfPath, rf, 0o600); err != nil {
		t.Fatal(err)
	}
	picks := 0
	orig := pickFile
	pickFile = func(context.Context, string) (string, error) { picks++; return rfPath, nil }
	t.Cleanup(func() { pickFile = orig })
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	e := newKeysEnv(t, out)
	const newPw = "the password after the restore"
	_, m := e.post(t, "api/encryption/restore", map[string]any{"from": "file", "recoveryKey": rk, "newPassword": newPw, "again": newPw})
	if m["ok"] != true || picks != 1 {
		t.Fatalf("the positive twin: the real recovery key opens the window and prepares: %v (windows %d)", m, picks)
	}
	e.post(t, "api/encryption/new-recovery-key/cancel", map[string]any{"token": m["token"]})
	_, m = e.post(t, "api/encryption/restore", map[string]any{"from": "file", "recoveryKey": string(rf), "newPassword": newPw, "again": newPw})
	if m["ok"] != false || m["error"] != recoveryFileInKeyBox {
		t.Errorf("the recovery file's contents in the key box: %v, want %q", m, recoveryFileInKeyBox)
	}
	_, statErr := os.Stat(filepath.Join(out, vault.FileName))
	assure.Reached(t, picks == 1 && os.IsNotExist(statErr) && !strings.Contains(toText(m), "argon2id"), "no window opened, nothing written, nothing quoted")
}

// covers: MA-1071, X11
// The page says which is which before anyone is confused (lead's words, owner-delegated 2026-11-02):
// the recovery key's box says it is the groups of letters and digits that are typed back; a line
// says the recovery file is something else — opened by MailArchive, never typed or pasted, and not
// holding the key; the save button names what the file is for; the type-back box and the restore
// box each ask for the recovery key's groups, not the recovery file, and the restore says MailArchive
// asks for the file itself. Positive twin first: the steps card is on the page.
func TestThePageTellsTheRecoveryKeyFromTheRecoveryFile(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	page := sessionPage(t, e.h)
	assure.Reached(t, strings.Contains(page, `id="encNewKeyBox"`), "the steps card")
	for _, want := range []string{
		`<p class="card-sub">Your new recovery key: the groups of letters and digits below. This is what you type back.</p>`,
		`<p class="card-sub" id="encFileWhat">` + recoveryFileWhat + `</p>`,
		`id="encSaveFileBtn" title="` + recoveryFileButtonTitle + `">Save the recovery file (for lost key files)…</button>`,
		`<label for="encTypedKey">Type the recovery key you saved: the groups of letters and digits, like ABCD-EFGH-…, not the recovery file</label>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the steps card does not say %q", want)
		}
	}
	if !strings.Contains(encryptionJS, "say(d.ok?('Saved the recovery file: '+d.path+'. '+d.note)") {
		t.Error("the saved line does not lead with what was saved")
	}
	// The restore's words, drawn for an archive whose key files are missing.
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	page = sessionPage(t, e.h)
	for _, want := range []string{
		`<label for="encRestoreKey">Recovery key: the groups of letters and digits you saved, like ABCD-EFGH-…, not the recovery file</label>`,
		`<p class="card-sub" id="encRestoreHow">` + restoreHowLine + `</p>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the restore does not say %q", want)
		}
	}
}
