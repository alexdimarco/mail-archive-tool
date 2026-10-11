package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/graph"
)

// Helpers shared by the copy-export engine's tests (extraction design §9).

// newCEEnv is an engine environment on a fresh state directory with the seams a
// test wants by default: no fsync (the sync seam of design §4.9 and MA-432), a
// 1 ms ticker and short windows, so a run takes milliseconds.
func newCEEnv(t *testing.T) *CopyExportEnv {
	t.Helper()
	e := NewCopyExportEnv(filepath.Join(t.TempDir(), "state"), "testhost", "vTEST")
	e.syncFile = func(*os.File) error { return nil }
	e.tick = time.Millisecond
	e.window = 300 * time.Millisecond
	e.suspendGap = 30 * time.Millisecond
	e.backoffMin, e.backoffMax = time.Millisecond, 20*time.Millisecond
	e.idle = 200 * time.Millisecond
	e.overall = 2 * time.Second
	return e
}

// ceBody is the bytes the fake serves for message id: its own text, so a byte-exact
// copy is checkable, and long enough to make the files distinct.
func ceBody(id string) []byte {
	return []byte("From: a@example.com\r\nSubject: subj-" + id + "\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\n\r\nbody of " + id + "\r\n")
}

// ceMsgs builds n planned messages in folder path, newest first, spread over months.
func ceMsgs(n int, path ...string) []CopyMessage {
	if len(path) == 0 {
		path = []string{"Inbox"}
	}
	var ref []CopyFolderRef
	for _, p := range path {
		ref = append(ref, CopyFolderRef{ID: "F-" + p, Name: p})
	}
	out := make([]CopyMessage, n)
	base := time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC)
	for i := range out {
		id := fmt.Sprintf("M%05d", i+1)
		out[i] = CopyMessage{SourceID: id, InternetMessageID: strings.ToLower(id) + "@x", Folder: ref, Subject: "subject " + id,
			From: "a@example.com", FromName: "A", Received: base.Add(-time.Duration(i) * 36 * time.Hour), SizeHint: int64(len(ceBody(id)))}
	}
	return out
}

// fakeSrc is a CopySource whose behaviour a test sets.
type fakeSrc struct {
	mu       sync.Mutex
	upn      string
	body     func(id string) []byte
	fetchFn  func(ctx context.Context, m *CopyMessage, w io.Writer, maxBytes int64) (int64, error)
	probeFn  func(ctx context.Context) error
	refresh  func(ctx context.Context) error
	signedIn func() (string, bool)
	fetches  map[string]int
	order    []string
}

func newFakeSrc() *fakeSrc {
	return &fakeSrc{upn: "alice@contoso.org", body: ceBody, fetches: map[string]int{}}
}

func (s *fakeSrc) Describe() CopySourceInfo {
	return CopySourceInfo{Kind: "m365", Mailbox: s.upn, Tenant: "tenant-1", ImmutableIDs: true,
		OriginalMeans: "MIME bytes returned by Microsoft Graph /$value at export time"}
}

func (s *fakeSrc) Fetch(ctx context.Context, m *CopyMessage, w io.Writer, maxBytes int64) (int64, error) {
	s.mu.Lock()
	s.fetches[m.SourceID]++
	s.order = append(s.order, m.SourceID)
	fn := s.fetchFn
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx, m, w, maxBytes)
	}
	b := s.body(m.SourceID)
	if maxBytes > 0 && int64(len(b)) > maxBytes {
		return 0, &graph.TooLargeError{MessageID: m.SourceID, Limit: maxBytes, Declared: int64(len(b))}
	}
	n, err := w.Write(b)
	return int64(n), err
}

func (s *fakeSrc) Probe(ctx context.Context) error {
	if s.probeFn != nil {
		return s.probeFn(ctx)
	}
	return nil
}

func (s *fakeSrc) RefreshAuth(ctx context.Context) error {
	if s.refresh != nil {
		return s.refresh(ctx)
	}
	return nil
}

func (s *fakeSrc) SignedIn() (string, bool) {
	if s.signedIn != nil {
		return s.signedIn()
	}
	return s.upn, true
}

func (s *fakeSrc) fetched(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetches[id]
}

// cePlan makes a plan of msgs into a fresh destination, with Preserve folders on.
func cePlan(t *testing.T, e *CopyExportEnv, src *fakeSrc, msgs []CopyMessage) *CopyPlan {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	return cePlanAt(t, e, src, msgs, dest)
}

func cePlanAt(t *testing.T, e *CopyExportEnv, src *fakeSrc, msgs []CopyMessage, dest string) *CopyPlan {
	t.Helper()
	p, err := e.PlanCopyExport(CopyPlanInput{Source: src.Describe(), Messages: msgs, Dest: dest,
		Options: CopyOptions{PreserveFolders: true}, Folders: []CopyPlanFolder{{Folder: "Inbox", Messages: len(msgs)}}})
	if err != nil {
		t.Fatalf("PlanCopyExport: %v", err)
	}
	return p
}

// ceRunPlan runs plan to its end.
func ceRunPlan(t *testing.T, e *CopyExportEnv, p *CopyPlan, src *fakeSrc) (*CopyResult, error) {
	t.Helper()
	return e.NewCopyRun(p, src).Run(context.Background())
}

// mustRun runs a plan and fails the test on a refusal.
func mustRun(t *testing.T, e *CopyExportEnv, p *CopyPlan, src *fakeSrc) *CopyResult {
	t.Helper()
	res, err := ceRunPlan(t, e, p, src)
	if err != nil {
		t.Fatalf("the run was refused: %v", err)
	}
	return res
}

// treeFiles lists every file under dir as slash paths relative to it, sorted, with
// its SHA-256, directories skipped. Symlinks are listed as such.
func treeFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			tgt, _ := os.Readlink(p)
			out[rel] = "symlink->" + tgt
		case d.IsDir():
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h := sha256.Sum256(b)
			out[rel] = hex.EncodeToString(h[:])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sumsCheck is an in-Go mirror of `sha256sum -c` plus the extra-files check (MA-421,
// MA-422): it parses SHA256SUMS.txt strictly (64 lowercase hex digits, two spaces, a
// forward-slash path), reports every listed file that is missing or changed, and
// every file under root that is not listed (the marker and the sums themselves
// excepted).
func sumsCheck(t *testing.T, root string) (bad, missing, extra []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, copySumsName))
	if err != nil {
		t.Fatalf("no %s: %v", copySumsName, err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) || bytes.Contains(data, []byte("\r")) || bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		t.Fatalf("%s is not LF-terminated UTF-8 without a BOM", copySumsName)
	}
	listed := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " {
			t.Fatalf("malformed sums line %q", line)
		}
		h, p := line[:64], line[66:]
		for _, c := range h {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				t.Fatalf("sums line %q: the hash is not lowercase hex", line)
			}
		}
		if strings.Contains(p, "\\") {
			t.Fatalf("sums line %q: a backslash in the path", line)
		}
		listed[p] = true
		paths = append(paths, p)
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		switch {
		case err != nil:
			missing = append(missing, p)
		default:
			sum := sha256.Sum256(b)
			if hex.EncodeToString(sum[:]) != h {
				bad = append(bad, p)
			}
		}
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("%s is not sorted by path", copySumsName)
	}
	for p := range treeFiles(t, root) {
		if p == copySumsName || p == copyMarkerName || listed[p] {
			continue
		}
		extra = append(extra, p)
	}
	sort.Strings(extra)
	return bad, missing, extra
}

// readText reads a file of the export.
func readText(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// exists reports whether a file exists under root.
func exists(root, rel string) bool {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

// tempResidue lists the .mailexport-*.tmp files left under root.
func tempResidue(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for p := range treeFiles(t, root) {
		if strings.Contains(filepath.Base(p), copyTempPrefix) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// manifestOf parses an export's export-manifest.json.
func manifestOf(t *testing.T, root string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(readText(t, root, copyManifestName)), &m); err != nil {
		t.Fatalf("export-manifest.json is not JSON: %v", err)
	}
	return m
}
