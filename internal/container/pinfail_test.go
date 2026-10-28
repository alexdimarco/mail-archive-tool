package container

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// covers: MA-854, R37
// A sealed file whose identity pin did not take is judged "could not be read", never
// passed (the P3-8 fixes' pass, WINPATH-1): on Windows its identity would be read only
// when the file is opened, from whatever is at the path then. A pin fails for real
// only in a race on Windows, so it is injected. Positive twin: the pin takes and the
// file is judged whole.
func TestAFileWhosePinDidNotTakeCouldNotBeRead(t *testing.T) {
	root := t.TempDir()
	body := []byte("a sealed file")
	os.MkdirAll(filepath.Join(root, "objects"), 0o755)
	os.WriteFile(filepath.Join(root, "objects", "f.bin"), body, 0o600)
	d := Digest{SHA256: sha256.Sum256(body), Size: int64(len(body))}
	if kind, detail := judgeFile(root, "objects/f.bin", d); kind != "" {
		t.Fatalf("positive twin: the file is whole: %s %s", kind, detail)
	}
	restore := util.FailPinsForTest()
	defer restore()
	kind, detail := judgeFile(root, "objects/f.bin", d)
	assure.Reached(t, kind == "modified" && detail == "could not be read", "judged: "+kind+" "+detail)
}
