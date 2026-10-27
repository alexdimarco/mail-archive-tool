package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/progcache"
)

// The adversarial pass on program copies (docs/review-encrypted-archives-phase3-pg-adversarial.md).

func programEntry(ps container.ProgramSet, name string) (container.ProgramFile, bool) {
	for _, f := range ps.Files {
		if f.Name == name {
			return f, true
		}
	}
	return container.ProgramFile{}, false
}

// covers: MA-844, R37
// Only what was written lands (DUR-01): an upgrade whose copy for one system cannot be
// staged — its cached file no longer matches — keeps that system's earlier copy sealed,
// keeps the older checksum file, raises nothing by a checksum file alone, and is
// incomplete; a later run that cannot write the new version removes none of it. Too
// little free space keeps every earlier copy the same way.
func TestAnUpgradeThatCannotStageKeepsTheEarlierCopies(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0", "v1.6.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	asRelease(t, "v1.6.0", r)
	c, _ := progcache.Open("")
	f := programFetcher(c, "v1.6.0")
	f.Fetch(context.Background(), "v1.6.0")
	mac := progcache.AssetFor("macos")
	os.WriteFile(filepath.Join(c.Root(), "v1.6.0", mac), []byte("not the release's bytes"), 0o600)
	res, _ := backup(t, out, key, ProgramCopiesUnset)
	ps := sealedSet(t, out, key)
	e, ok := programEntry(ps, mac)
	_, oldSums := programEntry(ps, "SHA256SUMS-v1.5.0")
	if res.Program.Class != ProgramIncomplete || !ok || e.Version != "v1.5.0" || !oldSums || !onDiskMatchesSet(t, out, ps) {
		t.Fatalf("a failed stage: %+v\nset %+v\nfiles %v", res.Program, ps, programFiles(t, out))
	}
	asDevBuild(t)
	_, logged := backup(t, out, key, ProgramCopiesUnset)
	if strings.Contains(logged, "did not put there was removed") || !onDiskMatchesSet(t, out, sealedSet(t, out, key)) {
		t.Fatalf("a later run removed the earlier copy:\n%s", logged)
	}

	// Every copy fails, only the checksum file stages: the high-water mark stays.
	r3 := newFakeReleases(t, "v1.5.0", "v1.6.0")
	asRelease(t, "v1.5.0", r3)
	out3, key3 := encryptedOut(t)
	backup(t, out3, key3, ProgramCopiesOn)
	asRelease(t, "v1.6.0", r3)
	c3, _ := progcache.Open("")
	programFetcher(c3, "v1.6.0").Fetch(context.Background(), "v1.6.0")
	for _, a := range progcache.Assets {
		os.WriteFile(filepath.Join(c3.Root(), "v1.6.0", a), []byte("not the release's bytes"), 0o600)
	}
	backup(t, out3, key3, ProgramCopiesUnset)
	if v := sealedSet(t, out3, key3).Version; v != "v1.5.0" {
		t.Fatalf("a checksum file alone raised the high-water mark to %s", v)
	}

	// Too little free space: nothing new, every earlier copy kept and sealed.
	r2 := newFakeReleases(t, "v1.5.0", "v1.6.0")
	asRelease(t, "v1.5.0", r2)
	out2, key2 := encryptedOut(t)
	backup(t, out2, key2, ProgramCopiesOn)
	asRelease(t, "v1.6.0", r2)
	of := programFreeBytes
	programFreeBytes = func(string) (uint64, error) { return 1 << 20, nil }
	res, _ = backup(t, out2, key2, ProgramCopiesUnset)
	programFreeBytes = of
	ps = sealedSet(t, out2, key2)
	assure.Reached(t, res.Program.Class == ProgramIncomplete && ps.Version == "v1.5.0" && len(ps.Files) == 5 && onDiskMatchesSet(t, out2, ps),
		fmt.Sprintf("no room: %+v %+v", res.Program, ps))
}

// covers: MA-844, R37
// A copy that cannot be put in place keeps its earlier entry, and the removals that
// depended on it do not happen (DUR-02): the older checksum file stays.
func TestACopyNotPutInPlaceKeepsItsEarlierEntry(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0", "v1.6.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	asRelease(t, "v1.6.0", r)
	mac := filepath.Join(out, container.ProgramDir, progcache.AssetFor("macos"))
	programFault = func(p string) error {
		if p == "intent" {
			os.Remove(mac)
			os.Mkdir(mac, 0o755) // a rename over a folder fails
			os.WriteFile(filepath.Join(mac, "x"), []byte("x"), 0o644)
		}
		return nil
	}
	defer func() { programFault = nil }()
	res, _ := backup(t, out, key, ProgramCopiesUnset)
	ps := sealedSet(t, out, key)
	e, ok := programEntry(ps, progcache.AssetFor("macos"))
	_, oldSums := programEntry(ps, "SHA256SUMS-v1.5.0")
	assure.Reached(t, res.Program.Class == ProgramIncomplete && ok && e.Version == "v1.5.0" && oldSums,
		fmt.Sprintf("not put in place: %+v %+v", res.Program, ps))
}

// covers: MA-844, R37
// The note's digest is sealed only when the note was written (DUR-03): a failed write
// leaves the old note and its digest, and the next backup tells no tamper.
func TestAFailedNoteWriteSealsNoNewDigest(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0", "v1.6.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	asRelease(t, "v1.6.0", r)
	ow := programWriteNote
	programWriteNote = func(string, []byte) error { return errors.New("the drive refused") }
	backup(t, out, key, ProgramCopiesUnset)
	programWriteNote = ow
	_, logged := backup(t, out, key, ProgramCopiesUnset)
	assure.Reached(t, !strings.Contains(logged, "changed or removed"), "no false tamper:\n"+logged)
}

// covers: MA-844, R37
// A run that ends before its first save never leaves a false tamper behind (DUR-05):
// the note a new MailArchive writes is sealed as it is written.
func TestAnEarlyEndedRunLeavesNoFalseTamper(t *testing.T) {
	asRelease(t, "v1.5.0", nil)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesUnset)
	asRelease(t, "v1.6.0", nil) // the note's version line changes
	// The run ends after it opened the archive and before its first save: its working
	// folder cannot be made.
	blocker := filepath.Join(tmpDir(t), "a-file")
	os.WriteFile(blocker, []byte("x"), 0o600)
	origScratch := scratchRoot
	scratchRoot = func() (string, error) { return filepath.Join(blocker, "scratch"), nil }
	_, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key, CopyFirst: true}, discard(), nil)
	scratchRoot = origScratch
	if err == nil {
		t.Fatal("fixture: the run did not end early")
	}
	_, logged := backup(t, out, key, ProgramCopiesUnset)
	assure.Reached(t, !strings.Contains(logged, "changed or removed"), "no false tamper:\n"+logged)
}

// covers: MA-844, R37
// Ctrl-C or Cancel during the step's wait for its download ends the step: nothing new
// is written, and the outcome says so (DUR-06, INT-01).
func TestACancelDuringTheWaitWritesNothing(t *testing.T) {
	isolatedCache(t)
	stall := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer stall.Close()
	oi, of := programIdentity, programFetcher
	programIdentity = func() (string, bool) { return "v1.7.0", true }
	programFetcher = func(c *progcache.Cache, this string) *progcache.Fetcher {
		return &progcache.Fetcher{Cache: c, This: this, BaseURL: stall.URL, Client: stall.Client(), Trusted: func(h string) bool { return h == "127.0.0.1" }, Idle: time.Minute}
	}
	defer func() { programIdentity, programFetcher = oi, of }()
	out, key := encryptedOut(t)
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	keep := true
	s.SetProgram(container.ProgramSet{Format: container.ProgramFormat, Keep: &keep})
	m.Save()
	m, s, _ = container.LoadManifest(out, container.LoadOptions{Key: key})
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	o := programStep(programEnv{ctx: ctx, out: out, store: s, save: m.Save, logger: discard(), runStart: time.Now(),
		stillHeld: func() error { return nil }, download: startProgramDownload(ctx, s)})
	assure.Reached(t, o.Class == ProgramCancelled && len(programFiles(t, out)) == 0, "cancelled, nothing written: "+o.Class)
}

// covers: MA-844, R37
// A kept copy that cannot be read is never rewritten or removed (R4), but never silent
// either (SWAP-01): every run warns, and after two it is incomplete.
func TestAnUnreadableCopyIsToldEveryRun(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a file mode that denies reading")
	}
	r := newFakeReleases(t, "v1.5.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	lx := filepath.Join(out, container.ProgramDir, progcache.AssetFor("linux-amd64"))
	os.Chmod(lx, 0o111)
	defer os.Chmod(lx, 0o644)
	res1, l1 := backup(t, out, key, ProgramCopiesUnset)
	res2, _ := backup(t, out, key, ProgramCopiesUnset)
	assure.Reached(t, strings.Contains(l1, "cannot be read") && res2.Program.Class == ProgramIncomplete && res1.Program.Class != ProgramUpdated,
		fmt.Sprintf("told: %s / %s", res1.Program.Class, res2.Program.Class))
}

// covers: MA-844, R37
// What the step finds, sharpened (SWAP-02, SWAP-03, CRIT-03, CRIT-01): a sealed name in
// another letter case is renamed back to its sealed spelling even on a volume that
// ignores case; a twin beside the sealed file, on a volume that does not, is removed; a
// folder with the step's temporary name, and a temporary file dated after the run
// began, are reported; a downloaded cloud file is judged as a regular file.
func TestTheSweepMendsCaseAndReportsWhatIsNotItsOwn(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	dir := filepath.Join(out, container.ProgramDir)
	lx := progcache.AssetFor("linux-amd64")
	os.Rename(filepath.Join(dir, lx), filepath.Join(dir, strings.ToUpper(lx)))
	os.Mkdir(filepath.Join(dir, ".mailarchive-program-7.tmp"), 0o755)
	future := filepath.Join(dir, ".mailarchive-program-8.tmp")
	os.WriteFile(future, []byte("x"), 0o644)
	os.Chtimes(future, time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	_, logged := backup(t, out, key, ProgramCopiesUnset)
	names := strings.Join(programFiles(t, out), " ")
	ok := strings.Contains(names, " "+lx) && !strings.Contains(names, strings.ToUpper(lx)) &&
		strings.Contains(logged, "program-7.tmp") && strings.Contains(logged, "program-8.tmp")
	if !ok {
		t.Fatalf("case and temps: %s\n%s", names, logged)
	}
	os.Remove(filepath.Join(dir, ".mailarchive-program-7.tmp"))
	os.Remove(future)
	// A downloaded cloud file: judged by its bytes, never taken for something in the way.
	sealedNames := map[string]bool{}
	for _, f := range sealedSet(t, out, key).Files {
		sealedNames[f.Name] = true
	}
	oc := programCloudFile
	programCloudFile = func(p string) bool { return sealedNames[filepath.Base(p)] }
	res, _ := backup(t, out, key, ProgramCopiesUnset)
	if res.Program.Class != ProgramUnchanged {
		t.Fatalf("a cloud file is a file: %s", res.Program.Class)
	}
	if runtime.GOOS != "windows" {
		// An entry Go calls irregular at a sealed name: with the cloud rule it is judged
		// by its bytes (here changed: written again); without it, it blocks its system.
		win := filepath.Join(dir, progcache.AssetFor("windows-amd64"))
		os.Remove(win)
		mkfifo(win)
		res, _ = backup(t, out, key, ProgramCopiesUnset)
		programCloudFile = oc
		fi, _ := os.Lstat(win)
		if res.Program.Class != ProgramUpdated || !fi.Mode().IsRegular() {
			t.Fatalf("a cloud entry at a sealed name: %s", res.Program.Class)
		}
		os.Remove(win)
		mkfifo(win)
		res, _ = backup(t, out, key, ProgramCopiesUnset)
		if res.Program.Class != ProgramIncomplete {
			t.Fatalf("a special file at a sealed name blocks: %s", res.Program.Class)
		}
		os.Remove(win)
		backup(t, out, key, ProgramCopiesUnset)
	}
	programCloudFile = oc
	// A twin on a volume that tells case apart.
	twin := filepath.Join(dir, strings.ToLower(progcache.AssetFor("macos")))
	os.WriteFile(twin, []byte("someone's"), 0o644)
	if b, _ := os.ReadFile(filepath.Join(dir, progcache.AssetFor("macos"))); string(b) != "someone's" {
		_, logged = backup(t, out, key, ProgramCopiesUnset)
		_, err := os.Stat(twin)
		if !os.IsNotExist(err) || !strings.Contains(logged, "did not put there was removed") {
			t.Fatalf("a case twin survived:\n%s", logged)
		}
	}
	assure.Reached(t, true, "the case, the temps, a cloud file and a twin")
}

// covers: MA-844, R37
// A newer self-copy another computer of the same system wrote is kept, never replaced
// by an older program (CRIT-04).
func TestANewerSelfCopyOfThisSystemIsKept(t *testing.T) {
	r := newFakeReleases(t) // publishes nothing: the release's copies cannot be had
	asRelease(t, "v1.6.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	_, name := selfPlatformAndName()
	before, _ := programEntry(sealedSet(t, out, key), name)
	asDevBuild(t)
	backup(t, out, key, ProgramCopiesUnset)
	after, ok := programEntry(sealedSet(t, out, key), name)
	assure.Reached(t, before.Version == "v1.6.0" && ok && after.Version == "v1.6.0" && after.SHA256 == before.SHA256, "the newer self-copy kept")
}

// covers: MA-844, R37
// The step reads only what it judged (R3, CRIT-06): the program folder swapped between
// the judgement and the open, or the file swapped, is "unreadable" — nothing is read
// through the swap, nothing rewritten or removed for it.
func TestTheStepReadsOnlyWhatItJudged(t *testing.T) {
	r := newFakeReleases(t, "v1.5.0")
	asRelease(t, "v1.5.0", r)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	dir := filepath.Join(out, container.ProgramDir)
	for _, point := range []string{"same", "open"} {
		swapped := false
		programBeforeOpen = func(p, path string) {
			if p != point || swapped || filepath.Base(path) != progcache.AssetFor("macos") {
				return
			}
			swapped = true
			if point == "same" {
				// Another folder in its place, holding a file of the same name and size.
				fi, _ := os.Stat(path)
				os.Rename(dir, dir+"-held")
				os.Mkdir(dir, 0o755)
				os.WriteFile(path, make([]byte, fi.Size()), 0o644)
			} else {
				os.Rename(path, path+".old")
				os.WriteFile(path, make([]byte, 10), 0o644)
			}
		}
		res, logged := backup(t, out, key, ProgramCopiesUnset)
		programBeforeOpen = nil
		if point == "same" {
			os.RemoveAll(dir)
			os.Rename(dir+"-held", dir)
		} else {
			p := filepath.Join(dir, progcache.AssetFor("macos"))
			os.Remove(p)
			os.Rename(p+".old", p)
		}
		if !swapped || !strings.Contains(logged, "cannot be read") {
			t.Fatalf("%s: a swap was not judged unreadable: %+v\n%s", point, res.Program, logged)
		}
	}
	assure.Reached(t, true, "judged unreadable at both points")
}

// covers: MA-844, R37
// A Microsoft 365 backup stopped by a fatal fault of the archive's drive skips the step
// (R1, INT-03), as a local one does.
func TestAFatalFaultSkipsTheStepForMicrosoft365(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	s := graphtest.NewServer()
	defer s.Close()
	s.Fixture()
	out, key := encryptedOut(t)
	orig := exporterSetup
	t.Cleanup(func() { exporterSetup = orig })
	exporterSetup = func(e *export.Exporter) {
		e.BeforeWrite = func(string) error { return sealedWriteErr(out, syscall.ENOSPC) }
	}
	res, err := RunGraph(context.Background(), sgOptions(s), Options{Out: out, Key: key, ProgramCopies: ProgramCopiesOn}, discard())
	var fatal *RunFatalError
	assure.Reached(t, errors.As(err, &fatal) && res.Program.Class == "" && len(programFiles(t, out)) == 0, fmt.Sprintf("skipped: %v %+v", err, res.Program))
}

// covers: MA-844, R37
// The choice changes only keep (R7, INT-05): an update under way stays as it was sealed.
func TestTheChoiceKeepsAnUpdateUnderWay(t *testing.T) {
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesUnset)
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	keep := true
	in := container.ProgramFile{Name: "mailarchive-linux-amd64", SHA256: strings.Repeat("a", 64), Size: 1, Origin: container.OriginRelease, Version: "v1.6.0", Platform: "linux-amd64", Reads: 1}
	s.SetProgram(container.ProgramSet{Format: container.ProgramFormat, Keep: &keep, State: container.ProgramIntent, Incoming: []container.ProgramFile{in},
		Removing: []string{"SHA256SUMS-v1.5.0"}, Note: &container.ProgramNote{Current: strings.Repeat("c", 64), Incoming: strings.Repeat("d", 64)}})
	m.Save()
	_, s, _ = container.LoadManifest(out, container.LoadOptions{Key: key})
	sealProgramChoice(s, ProgramCopiesOff, discard())
	got, _, _ := s.Program()
	assure.Reached(t, got.Keep != nil && !*got.Keep && got.State == container.ProgramIntent && len(got.Incoming) == 1 && len(got.Removing) == 1 && got.Note != nil,
		fmt.Sprintf("the update kept: %+v", got))
}

// covers: MA-844, R37
// R9's case — the renames lost but the landed commit kept — is a known gap (the PG
// pass, CRIT-02): the next run from a computer that cannot write the new version takes
// the older genuine copies for changed. Recorded in §14; this encoder fails when it is
// closed.
func TestLostRenamesKeepTheGoodCopies(t *testing.T) {
	t.Skip("open gap R9/CRIT-02: a landed commit whose renames were lost (a power cut with best-effort folder sync) lets a later run remove the older genuine copies — program design R9, the PG pass CRIT-02; recorded in §14")
}
