package vault

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-755, R37
// FC-4: an I/O error on one reserved name does not end the recognition: a later
// signal (here the unit store) still answers encrypted; the error is returned only
// when no signal does.
func TestEncryptedMarkerLooksPastAnErrorOnOneName(t *testing.T) {
	dir := t.TempDir()
	shard := filepath.Join(dir, unitsDirName, "ab")
	if err := os.MkdirAll(shard, 0o700); err != nil {
		t.Fatal(err)
	}
	writeIn(t, shard, "ab"+strings.Repeat("0", 30), []byte("sealed"))
	boom := errors.New("an I/O error on one name")
	orig := markerLstat
	markerLstat = func(p string) (fs.FileInfo, error) {
		if filepath.Base(p) == BackupName {
			return nil, boom
		}
		return orig(p)
	}
	defer func() { markerLstat = orig }()
	if enc, why, err := EncryptedMarker(dir); !enc || err != nil || !strings.Contains(why, unitsDirName) {
		t.Fatalf("a later signal must still answer encrypted: %v %q %v", enc, why, err)
	}
	empty := t.TempDir()
	if enc, _, err := EncryptedMarker(empty); enc || !errors.Is(err, boom) {
		t.Fatalf("with no signal the error is returned: %v %v", enc, err)
	}
}
