package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-175, MA-386, R12, S33
// Every rebuild remedy quotes the archive path, so a pasted command survives a
// folder with a space in its name (g7 friction re-verification of F5): plain
// `reindex` on a lost index beside an intact manifest printed `-out %s`
// unquoted, the defect F5 fixed in its neighbouring sentence. (The positive twin
// is TestRebuildRefusalsAreLegible: the same refusal still names the rebuild.)
func TestReindexLostIndexRemedyQuotesThePath(t *testing.T) {
	out := filepath.Join(tmpDir(t), "my mail archive")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	buildArchive(t, out)
	deleteSearchDB(t, out)
	_, _, err := Reindex(out, log.New(io.Discard, "", 0))
	code, msg := refusalOf(err)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("reindex -rebuild -out "+fmt.Sprintf("%q", out)),
		assure.NoSideEffect(func() bool {
			_, e := os.Stat(filepath.Join(out, "search.db"))
			return os.IsNotExist(e)
		}))
}
