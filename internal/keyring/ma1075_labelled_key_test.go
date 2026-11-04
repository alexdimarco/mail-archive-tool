package keyring

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
)

// covers: MA-1075, R37
// A recovery key pasted the way a note or a password manager often holds it — with a label
// ("Recovery key: …"), in quotes, or both — is not told as the recovery file's contents (the
// independent review's LOW-1 of bc84ca5: any colon read as the file's, so "Recovery key: ABCD-…" was
// answered "That is the recovery file's contents"). Positive twins first: the whole recovery file,
// and each of its lines, still read as the file's.
func TestALabelledOrQuotedRecoveryKeyIsNotTheRecoveryFile(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	p, err := Create(filepath.Join(t.TempDir(), "Enc"), "a test passphrase here", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Discard()
	key := p.RecoveryKey()
	data, err := p.RecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	file := string(data)
	assure.Reached(t, LooksLikeRecoveryFile(file), "the positive twin: the whole recovery file reads as one")
	for _, line := range strings.Split(strings.TrimSpace(file), "\n") {
		if strings.TrimSpace(line) != "" && !LooksLikeRecoveryFile(line) {
			t.Errorf("the positive twin: a line of the recovery file does not read as one: %q", line)
		}
	}
	for _, typed := range []string{"Recovery key: " + key, "recovery key:" + key, `"` + key + `"`, `Recovery key: "` + key + `"`, "Key: " + strings.ToLower(key), "MailArchive (Enc): " + key} {
		if LooksLikeRecoveryFile(typed) {
			t.Errorf("%q reads as the recovery file's contents", typed)
		}
	}
}
