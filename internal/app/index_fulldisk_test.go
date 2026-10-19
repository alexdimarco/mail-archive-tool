//go:build darwin || linux

package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// fullDiskChildEnv marks the re-executed test binary that runs the capped part
// of TestIndexOpenRefusalNamesTheFullDiskShape.
const fullDiskChildEnv = "MAILARCHIVE_TEST_FULLDISK_CHILD"

// fullDiskOutcome is what the capped child reports back: the text of the two
// refusals ("" for none).
type fullDiskOutcome struct {
	Run     string
	Rebuild string
}

// runUnderFileSizeLimit is the capped half of the full-disk test, run in a
// child process whose RLIMIT_FSIZE is lowered to 24 KB — a write past that
// offset fails with EFBIG, the full-disk model the friction walk used. The limit
// is on the child alone: lowering it in the test process made Go's own test log
// (testlog.txt, written while the unfiltered `go test ./...` caches results)
// fail with "file too large", failing the whole package whenever a flush landed
// in the window. The Go runtime does not die of SIGXFSZ (notify-only), so the
// writes just fail.
func runUnderFileSizeLimit(t *testing.T, src, out string) fullDiskOutcome {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestIndexOpenRefusalNamesTheFullDiskShape$", "-test.count=1")
	cmd.Env = append(os.Environ(), fullDiskChildEnv+"="+out, fullDiskChildEnv+"_SRC="+src)
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("the capped child failed: %v\n%s", err, stdout)
	}
	for _, line := range strings.Split(string(stdout), "\n") {
		if strings.HasPrefix(line, "{") {
			var o fullDiskOutcome
			if err := json.Unmarshal([]byte(line), &o); err != nil {
				t.Fatalf("the capped child's report %q: %v", line, err)
			}
			return o
		}
	}
	t.Fatalf("the capped child reported nothing:\n%s", stdout)
	return fullDiskOutcome{}
}

// fullDiskChild runs in the child process: lower the limit (24 KB, the walked
// figure), run and rebuild, print the two refusals as one JSON line.
func fullDiskChild(t *testing.T, out string) {
	src := os.Getenv(fullDiskChildEnv + "_SRC")
	capped := syscall.Rlimit{Cur: 24 << 10, Max: 24 << 10}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &capped); err != nil {
		t.Skipf("cannot lower RLIMIT_FSIZE: %v", err)
	}
	var o fullDiskOutcome
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		o.Run = err.Error()
	}
	if _, err := Rebuild(out, discard()); err != nil {
		o.Rebuild = err.Error()
	}
	b, _ := json.Marshal(o)
	os.Stdout.Write(append(b, '\n'))
}

// covers: MA-386, R12, S33
// The walked full-disk shape (F1): with writes past 24 KB refused (a full disk
// modelled by RLIMIT_FSIZE, as the friction walk did), a run's writable
// open of search.db fails when SQLite cannot grow the -shm
// (SQLITE_IOERR_SHMSIZE, "disk I/O error (4874)"). The refusal names the
// file, SQLite's cause, the full disk and the remedy (free space), never the
// rebuild, and writes nothing: search.db and the manifest keep their bytes.
// The rebuild, run on that disk anyway, names the same cause and leaves no
// search.db.rebuild behind. Followed (space freed), the next run indexes
// every record. Positive twin first: the run with room on the disk.
func TestIndexOpenRefusalNamesTheFullDiskShape(t *testing.T) {
	if childOut := os.Getenv(fullDiskChildEnv); childOut != "" {
		fullDiskChild(t, childOut)
		return
	}
	opts, healthy, unchanged := unwritableFixture(t, 3)
	out, idxPath := opts.Out, filepath.Join(opts.Out, "search.db")

	o := runUnderFileSizeLimit(t, opts.Inputs[0], out)
	var runErr, rebuildErr error
	if o.Run != "" {
		runErr = childError(o.Run)
	}
	if o.Rebuild != "" {
		rebuildErr = childError(o.Rebuild)
	}
	code, msg := refusalOf(runErr)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, "disk I/O error", "the disk holding "+out+" is full or failing", "free space", "run again"),
		assure.Forbid("reindex -rebuild", "rebuild it"),
		assure.NoSideEffect(unchanged))
	rcode, rmsg := refusalOf(rebuildErr)
	assure.Refused(t, rcode, rmsg, assure.Code(1),
		assure.Names("search.db.rebuild", "the disk holding "+out+" is full or failing", "free space"),
		assure.NoSideEffect(func() bool { return unchanged() && noRebuildTemp(out) }))

	res, err := Run(context.Background(), opts, discard(), nil)
	if err != nil || res.Indexed != res.ManifestSize || res.Indexed != healthy.Indexed {
		t.Fatalf("after freeing space: indexed=%d of %d, err=%v; want all %d", res.Indexed, res.ManifestSize, err, healthy.Indexed)
	}
}

// childError is an error carrying text reported by the child process.
type childError string

func (e childError) Error() string { return string(e) }
