package keyring

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
)

// covers: MA-1071, R37
// A recovery file's contents, typed or pasted where a recovery key is asked, are told from a
// recovery key (MA-1071): the whole file and each of its lines read as the file's; a recovery key
// does not, however it is typed (letter case, spaces, no dashes). Positive twin first: the real
// recovery key, as shown, is a recovery key.
func TestARecoveryFilesContentsAreToldFromARecoveryKey(t *testing.T) {
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
	if RecoveryKeyProblem(key) != nil || LooksLikeRecoveryFile(key) {
		t.Fatalf("the positive twin: the recovery key as shown is a recovery key, not a recovery file")
	}
	for _, typed := range []string{strings.ToLower(key), strings.ReplaceAll(key, "-", " "), strings.ReplaceAll(key, "-", ""), "", "ABCD-EFGH"} {
		if LooksLikeRecoveryFile(typed) {
			t.Errorf("%q reads as a recovery file", typed)
		}
	}
	file := string(data)
	if !strings.Contains(file, `"kind": "`+recoveryFileKind+`"`) {
		t.Errorf("a recovery file does not name the kind %q this reads", recoveryFileKind)
	}
	assure.Reached(t, LooksLikeRecoveryFile(file), "the whole recovery file reads as one")
	for _, line := range strings.Split(strings.TrimSpace(file), "\n") {
		if strings.TrimSpace(line) != "" && !LooksLikeRecoveryFile(line) {
			t.Errorf("a line of the recovery file does not read as one: %q", line)
		}
	}
}
