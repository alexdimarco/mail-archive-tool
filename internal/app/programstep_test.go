package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/progcache"
	"mail-archive-tool/internal/vault"
)

// fakeReleases serves releases: per version, the four assets and SHA256SUMS.
type fakeReleases struct {
	files map[string][]byte
	srv   *httptest.Server
}

// isolatedCache gives the test a program cache of its own: the cache's expects
// version and pruning carry over between tests otherwise.
func isolatedCache(t *testing.T) {
	t.Helper()
	done := progcache.UseTestRoot(filepath.Join(t.TempDir(), "program-cache"))
	t.Cleanup(func() { done() })
}

func newFakeReleases(t *testing.T, versions ...string) *fakeReleases {
	t.Helper()
	isolatedCache(t)
	r := &fakeReleases{files: map[string][]byte{}}
	for _, v := range versions {
		var sums strings.Builder
		for i, a := range progcache.Assets {
			b := []byte(fmt.Sprintf("MailArchive %s %s %d", v, a, i))
			r.files[v+"/"+a] = b
			s := sha256.Sum256(b)
			fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(s[:]), a)
		}
		r.files[v+"/"+progcache.SumsName] = []byte(sums.String())
	}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, ok := r.files[strings.TrimPrefix(req.URL.Path, "/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// asRelease makes this program a release build of version, downloading from r.
func asRelease(t *testing.T, version string, r *fakeReleases) {
	t.Helper()
	oi, of := programIdentity, programFetcher
	programIdentity = func() (string, bool) { return version, true }
	if r != nil {
		programFetcher = func(c *progcache.Cache, this string) *progcache.Fetcher {
			return &progcache.Fetcher{Cache: c, This: this, BaseURL: r.srv.URL, Client: r.srv.Client(), Trusted: func(h string) bool { return h == "127.0.0.1" }}
		}
	}
	t.Cleanup(func() { programIdentity, programFetcher = oi, of })
}

// asDevBuild makes this program a development build.
func asDevBuild(t *testing.T) {
	t.Helper()
	oi := programIdentity
	programIdentity = func() (string, bool) { return "(devel)", false }
	t.Cleanup(func() { programIdentity = oi })
}

func backup(t *testing.T, out string, key *vault.Key, c ProgramChoice) (Result, string) {
	t.Helper()
	var logged bytes.Buffer
	res, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key, ProgramCopies: c}, log.New(&logged, "", 0), nil)
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, logged.String())
	}
	return res, logged.String()
}

func sealedSet(t *testing.T, out string, key *vault.Key) container.ProgramSet {
	t.Helper()
	_, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	ps, _, _ := s.Program()
	return ps
}

// programFiles lists program/ by name.
func programFiles(t *testing.T, out string) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(out, container.ProgramDir))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// onDiskMatchesSet says every sealed file is on disk with its sealed bytes, and nothing else.
func onDiskMatchesSet(t *testing.T, out string, ps container.ProgramSet) bool {
	t.Helper()
	want := map[string]string{}
	for _, f := range ps.Files {
		want[f.Name] = f.SHA256
	}
	got := programFiles(t, out)
	if len(got) != len(want) {
		return false
	}
	for _, n := range got {
		b, err := os.ReadFile(filepath.Join(out, container.ProgramDir, n))
		s := sha256.Sum256(b)
		if err != nil || want[n] != hex.EncodeToString(s[:]) {
			return false
		}
	}
	return true
}

// covers: MA-840, R37
// A release build keeps the release's copies (program design §4, §5): the first backup
// with copies chosen downloads the release into the cache, writes the four copies and
// SHA256SUMS-<version> into program/, byte for byte, seals them landed at the version,
// and names them in the note; a second backup changes nothing. The step runs after
// the backup's last save and never fails it.
func TestAReleaseKeepsItsCopiesInTheArchive(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	res, _ := backup(t, out, key, ProgramCopiesOn)
	ps := sealedSet(t, out, key)
	note, _ := os.ReadFile(filepath.Join(out, "README.txt"))
	ok := res.Program.Class == ProgramUpdated && ps.State == container.ProgramLanded && ps.Version == "v1.5.0" &&
		len(ps.Files) == 5 && onDiskMatchesSet(t, out, ps) && strings.Contains(string(note), "for Mac: MailArchive v1.5.0") &&
		strings.Contains(string(note), "SHA256SUMS-v1.5.0")
	if !ok {
		t.Fatalf("first backup: %+v\nset %+v\nfiles %v\nnote:\n%s", res.Program, ps, programFiles(t, out), note)
	}
	b, _ := os.ReadFile(filepath.Join(out, container.ProgramDir, "SHA256SUMS-v1.5.0"))
	res, logged := backup(t, out, key, ProgramCopiesUnset)
	assure.Reached(t, bytes.Equal(b, r.files["v1.5.0/SHA256SUMS"]) && res.Program.Class == ProgramUnchanged && !strings.Contains(logged, "changed or removed"),
		"the second backup changes nothing: "+res.Program.Class)
}

// covers: MA-840, R37
// An upgrade replaces every copy with the newer release's and its checksum file the
// older one's; the high-water mark rises to it.
func TestAnUpgradeReplacesTheCopies(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0", "v1.6.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	asRelease(t, "v1.6.0", r)
	res, _ := backup(t, out, key, ProgramCopiesUnset)
	ps := sealedSet(t, out, key)
	files := strings.Join(programFiles(t, out), " ")
	assure.Reached(t, res.Program.Class == ProgramUpdated && ps.Version == "v1.6.0" && onDiskMatchesSet(t, out, ps) &&
		strings.Contains(files, "SHA256SUMS-v1.6.0") && !strings.Contains(files, "SHA256SUMS-v1.5.0"), "upgraded: "+files)
}

// covers: MA-840, R37
// A development build keeps this computer's own program (C5, R14): no release copy;
// the self-copy, its bytes the program's own and, bare, executable; "cannot be checked"
// in the note. Copies off then remove it, and the empty program folder.
func TestADevelopmentBuildKeepsItsOwnProgram(t *testing.T) {
	asDevBuild(t)
	out, key := encryptedOut(t)
	res, _ := backup(t, out, key, ProgramCopiesOn)
	ps := sealedSet(t, out, key)
	plat, name := selfPlatformAndName()
	exe, _ := selfExecutable()
	want, _ := os.ReadFile(exe)
	got, _ := os.ReadFile(filepath.Join(out, container.ProgramDir, name))
	fi, _ := os.Stat(filepath.Join(out, container.ProgramDir, name))
	note, _ := os.ReadFile(filepath.Join(out, "README.txt"))
	ok := res.Program.Class == ProgramUpdated && len(ps.Files) == 1 && ps.Files[0].Origin == container.OriginSelf && ps.Files[0].Platform == plat &&
		bytes.Equal(got, want) && onDiskMatchesSet(t, out, ps) && strings.Contains(string(note), "this computer's own program, a development build — it cannot be checked") &&
		(runtime.GOOS == "windows" || fi.Mode().Perm()&0o100 != 0)
	if !ok {
		t.Fatalf("self-copy: %+v %+v", res.Program, ps)
	}
	res, _ = backup(t, out, key, ProgramCopiesOff)
	_, gone := os.Stat(filepath.Join(out, container.ProgramDir))
	note, _ = os.ReadFile(filepath.Join(out, "README.txt"))
	assure.Reached(t, len(sealedSet(t, out, key).Files) == 0 && os.IsNotExist(gone) && !strings.Contains(string(note), "Copies of MailArchive"),
		"copies off: removed, and the folder")
}

// covers: MA-840, R37
// The step runs for a Microsoft 365 backup too (R1), and is skipped by a run that was
// cancelled.
func TestTheStepRunsForMicrosoft365AndNotWhenCancelled(t *testing.T) {
	asDevBuild(t)
	s := sweepServer(false, []string{"A1"}, nil, nil)
	defer s.Close()
	out, key := encryptedOut(t)
	res, err := RunGraph(context.Background(), isoOptions(s), Options{Out: out, Key: key, ProgramCopies: ProgramCopiesOn}, discard())
	if err != nil || res.Program.Class != ProgramUpdated || len(programFiles(t, out)) != 1 {
		t.Fatalf("Microsoft 365: %v %+v %v", err, res.Program, programFiles(t, out))
	}
	out2, key2 := encryptedOut(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, _ = Run(ctx, Options{Inputs: []string{encryptedSourcePST(t)}, Out: out2, Key: key2, ProgramCopies: ProgramCopiesOn}, discard(), nil)
	assure.Reached(t, res.Program.Class == "" && len(programFiles(t, out2)) == 0, "a cancelled run skips the step")
}

// covers: MA-841, R37
// A stop anywhere in the staged protocol leaves an archive the next backup finishes
// (§4.3, R5, R7, R9): after staging, after the intent commit, after a rename, after the
// renames — the next run seals a landed set whose files are on disk as sealed, tells
// no tamper of the note, and leaves no temporary file.
func TestAStopInTheProtocolIsFinishedByTheNextBackup(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0", "v1.6.0")
	for _, point := range []string{"staged", "intent", "rename", "renamed"} {
		t.Run(point, func(t *testing.T) {
			asRelease(t, "v1.5.0", r)
			out, key := encryptedOut(t)
			backup(t, out, key, ProgramCopiesOn)
			asRelease(t, "v1.6.0", r)
			programFault = func(p string) error {
				if p == point {
					return errors.New("stopped here")
				}
				return nil
			}
			res, _ := backup(t, out, key, ProgramCopiesUnset)
			programFault = nil
			if res.Program.Class != ProgramStopped {
				t.Fatalf("the stop: %+v", res.Program)
			}
			res, logged := backup(t, out, key, ProgramCopiesUnset)
			ps := sealedSet(t, out, key)
			files := programFiles(t, out)
			assure.Reached(t, ps.State == container.ProgramLanded && onDiskMatchesSet(t, out, ps) && ps.Version == "v1.6.0" &&
				!strings.Contains(logged, "changed or removed") && !strings.Contains(strings.Join(files, " "), ".tmp"),
				fmt.Sprintf("finished: %+v %v %s", res.Program, files, logged))
		})
	}
}

// covers: MA-841, R37
// An update a stopped run left is finished from what is on the drive even when the
// next run cannot write the copies again — a development build (R7): each file
// matching its incoming entry is accepted, none is taken for changed or removed.
func TestAStoppedUpdateIsAcceptedWithoutTheCache(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0", "v1.6.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	asRelease(t, "v1.6.0", r)
	programFault = func(p string) error {
		if p == "renamed" {
			return errors.New("stopped here")
		}
		return nil
	}
	backup(t, out, key, ProgramCopiesUnset)
	programFault = nil
	asDevBuild(t)
	_, logged := backup(t, out, key, ProgramCopiesUnset)
	ps := sealedSet(t, out, key)
	assure.Reached(t, ps.State == container.ProgramLanded && ps.Version == "v1.6.0" && onDiskMatchesSet(t, out, ps) && len(ps.Files) == 5 &&
		!strings.Contains(logged, "was changed") && !strings.Contains(logged, "changed or removed"), "the update accepted:\n"+logged)
}

// covers: MA-841, R37
// What the step finds in program/ (§4.1, R3, R4, R8, R12, R13): a file of the pattern it
// did not put there is removed with a warning; a folder is reported and left; the
// system's litter is ignored; a sealed name in another letter case is renamed back; a
// changed copy is written again from the cache; an online-only copy is neither opened
// nor rewritten; a folder at a sealed name blocks its platform; a link at program/ is
// never followed — the step leaves it and warns.
func TestWhatTheStepFindsInTheProgramFolder(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	dir := filepath.Join(out, container.ProgramDir)
	mac := progcache.AssetFor("macos")
	os.WriteFile(filepath.Join(dir, "planted-program"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, "MailArchive.app"), 0o755)
	os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o644)
	oldTemp := filepath.Join(dir, ".mailarchive-program-5.tmp") // a crashed run's
	os.WriteFile(oldTemp, []byte("half"), 0o644)
	os.Chtimes(oldTemp, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	os.WriteFile(filepath.Join(dir, progcache.AssetFor("windows-amd64")), []byte("swapped"), 0o644)
	os.Rename(filepath.Join(dir, progcache.AssetFor("linux-amd64")), filepath.Join(dir, strings.ToUpper(progcache.AssetFor("linux-amd64"))))
	res, logged := backup(t, out, key, ProgramCopiesUnset)
	ps := sealedSet(t, out, key)
	_, planted := os.Stat(filepath.Join(dir, "planted-program"))
	_, app := os.Stat(filepath.Join(dir, "MailArchive.app"))
	_, litter := os.Stat(filepath.Join(dir, ".DS_Store"))
	win, _ := os.ReadFile(filepath.Join(dir, progcache.AssetFor("windows-amd64")))
	_, lx := os.Stat(filepath.Join(dir, progcache.AssetFor("linux-amd64")))
	_, temp := os.Stat(oldTemp)
	ok := os.IsNotExist(planted) && os.IsNotExist(temp) && app == nil && litter == nil && bytes.Equal(win, r.files["v1.5.0/"+progcache.AssetFor("windows-amd64")]) && lx == nil &&
		strings.Contains(logged, "did not put there was removed: planted-program") && strings.Contains(logged, "was left as it is") && len(ps.Files) == 5 &&
		!strings.Contains(logged, "DS_Store") && !strings.Contains(strings.ToLower(logged), "removed: mailarchive-linux")
	if !ok {
		t.Fatalf("found: %+v (planted %v temp %v app %v litter %v win %v lx %v files %d)\n%s", res.Program, planted, temp, app, litter,
			bytes.Equal(win, r.files["v1.5.0/"+progcache.AssetFor("windows-amd64")]), lx, len(ps.Files), logged)
	}

	// An online-only copy: never opened, never rewritten.
	op := programPlaceholder
	programPlaceholder = func(p string) bool { return filepath.Base(p) == mac }
	os.WriteFile(filepath.Join(dir, mac), []byte("not downloaded"), 0o644)
	res, _ = backup(t, out, key, ProgramCopiesUnset)
	programPlaceholder = op
	b, _ := os.ReadFile(filepath.Join(dir, mac))
	ps = sealedSet(t, out, key)
	var since string
	for _, f := range ps.Files {
		if f.Name == mac {
			since = f.UncheckedSince
		}
	}
	if string(b) != "not downloaded" || since == "" {
		t.Fatalf("a placeholder was rewritten or not marked: %q %q %+v", b, since, res.Program)
	}

	// A folder at a sealed name blocks its platform.
	os.Remove(filepath.Join(dir, mac))
	os.Mkdir(filepath.Join(dir, mac), 0o755)
	res, logged = backup(t, out, key, ProgramCopiesUnset)
	if res.Program.Class != ProgramIncomplete || !strings.Contains(logged, "where the copy for Mac belongs") {
		t.Fatalf("blocked: %+v\n%s", res.Program, logged)
	}
	os.Remove(filepath.Join(dir, mac))

	// A link at program/ is never followed.
	if runtime.GOOS != "windows" {
		elsewhere := t.TempDir()
		os.Rename(dir, dir+"-moved")
		os.Symlink(elsewhere, dir)
		res, logged = backup(t, out, key, ProgramCopiesUnset)
		entries, _ := os.ReadDir(elsewhere)
		assure.Reached(t, res.Program.Class == ProgramSkipped && len(entries) == 0 && strings.Contains(logged, "is a link"), "a link is never followed: "+res.Program.Class)
	}
}

// covers: MA-841, R37
// A lock lost before the step writes stops it with nothing written (R2): no program
// folder, nothing recorded.
func TestALostLockStopsTheStepBeforeAnyWrite(t *testing.T) {
	asDevBuild(t)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesUnset)
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	keep := true
	s.SetProgram(container.ProgramSet{Format: container.ProgramFormat, Keep: &keep})
	m.Save()
	m, s, _ = container.LoadManifest(out, container.LoadOptions{Key: key})
	o := programStep(programEnv{ctx: context.Background(), out: out, store: s, save: m.Save, logger: discard(), runStart: time.Now(),
		stillHeld: func() error { return errors.New("the lock was taken") }})
	_, dirErr := os.Stat(filepath.Join(out, container.ProgramDir))
	assure.Reached(t, o.Class == ProgramStopped && os.IsNotExist(dirErr) && len(sealedSet(t, out, key).Files) == 0, "stopped, nothing written: "+o.Class)
}

// covers: MA-841, R37
// A set this MailArchive cannot read leaves program/ untouched (R6), and an archive
// whose copies were never chosen gets none.
func TestTheStepLeavesWhatItCannotRead(t *testing.T) {
	asDevBuild(t)
	out, key := encryptedOut(t)
	res, _ := backup(t, out, key, ProgramCopiesUnset)
	if res.Program.Class != ProgramNotChosen || len(programFiles(t, out)) != 0 {
		t.Fatalf("not chosen: %+v", res.Program)
	}
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	newer := `{"format":2,"keep":true,"shiny":1}`
	s.SetProgramRawForTest([]byte(newer))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(out, container.ProgramDir), 0o755)
	os.WriteFile(filepath.Join(out, container.ProgramDir, "planted-program"), []byte("x"), 0o644)
	res, logged := backup(t, out, key, ProgramCopiesUnset)
	_, s2, _ := container.LoadManifest(out, container.LoadOptions{Key: key, ReadOnly: true})
	_, planted := os.Stat(filepath.Join(out, container.ProgramDir, "planted-program"))
	_ = io.Discard
	assure.Reached(t, res.Program.Class == ProgramKeptAsIs && planted == nil && string(s2.ProgramRaw()) == newer && strings.Contains(logged, "cannot read"),
		"a set it cannot read leaves program/ untouched: "+res.Program.Class)
}
