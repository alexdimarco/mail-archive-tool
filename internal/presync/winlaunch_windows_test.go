//go:build windows

package presync

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows start through CreateProcessW (design-presync-attended §2.7, MA-923;
// lab L-A10 2026-10-31): what Windows is asked for, the two handles it returns, and
// a real start of a stand-in program that reports what it received. The pure
// builder is golden-tested on every platform in winlaunch_test.go.

// startHelperEnv names the file a stand-in program writes its report to: when it is
// set, this test binary is the stand-in (TestMain), not a test run.
const startHelperEnv = "MAILARCHIVE_TEST_START_REPORT"

// startReport is what the stand-in received from Windows.
type startReport struct {
	Raw   string   // GetCommandLineW, as Windows passed it
	Argv  []string // the raw line split by CommandLineToArgvW (what decodeArgv uses)
	Args  []string // os.Args, Go's own split
	Dir   string   // its working folder
	Flags uint32   // its STARTUPINFO.dwFlags
	Show  uint16   // its STARTUPINFO.wShowWindow
}

func TestMain(m *testing.M) {
	if out := os.Getenv(startHelperEnv); out != "" {
		os.Exit(reportStart(out))
	}
	os.Exit(m.Run())
}

// reportStart is the stand-in's whole run: it writes what it received and exits.
func reportStart(out string) int {
	var si windows.StartupInfo
	_ = windows.GetStartupInfo(&si)
	wd, _ := os.Getwd()
	raw := windows.UTF16PtrToString(windows.GetCommandLine())
	argv, _ := windows.DecomposeCommandLine(raw)
	b, err := json.Marshal(startReport{Raw: raw, Argv: argv, Args: os.Args, Dir: wd, Flags: si.Flags, Show: si.ShowWindow})
	if err != nil {
		return 2
	}
	if os.WriteFile(out+".part", b, 0o600) != nil || os.Rename(out+".part", out) != nil {
		return 3
	}
	return 0
}

// createCall is one CreateProcess request as the fake saw it.
type createCall struct {
	app, cmdLine, dir string
	inherit           bool
	flags             uint32
	si                windows.StartupInfo
}

// fakeCreateProcess replaces the two Win32 calls of a start for the test: each
// CreateProcess request is recorded; with fail nil it "starts" and hands back
// process handle 0x111 and thread handle 0x222, otherwise it fails with fail. Every
// CloseHandle is recorded.
func fakeCreateProcess(t *testing.T, fail error) (calls *[]createCall, closed *[]windows.Handle) {
	t.Helper()
	oldCreate, oldClose := winCreateProcess, winCloseHandle
	t.Cleanup(func() { winCreateProcess, winCloseHandle = oldCreate, oldClose })
	calls, closed = &[]createCall{}, &[]windows.Handle{}
	winCreateProcess = func(app, line *uint16, inherit bool, flags uint32, dir *uint16, si *windows.StartupInfo, pi *windows.ProcessInformation) error {
		c := createCall{app: windows.UTF16PtrToString(app), cmdLine: windows.UTF16PtrToString(line), inherit: inherit, flags: flags, si: *si}
		if dir != nil {
			c.dir = windows.UTF16PtrToString(dir)
		}
		*calls = append(*calls, c)
		if fail != nil {
			return fail
		}
		pi.Process, pi.Thread, pi.ProcessId = 0x111, 0x222, 4242
		return nil
	}
	winCloseHandle = func(h windows.Handle) error {
		*closed = append(*closed, h)
		return nil
	}
	return calls, closed
}

// covers: MA-923
// One start is one CreateProcess request asking for exactly what the builder built
// — the full program path, the command line, the install folder, no handle
// inherited, CREATE_BREAKAWAY_FROM_JOB, and a STARTUPINFO of its own size carrying
// only STARTF_USESHOWWINDOW with SW_SHOWMINNOACTIVE — and both handles Windows
// returns, the thread's and the process's, are closed once each, so the long-lived
// Desktop keeps none. Positive twin of the failure case: a refused start closes
// nothing, since Windows returned nothing.
func TestWinStartRequestAndHandles(t *testing.T) {
	const prof = `C:\Users\First Last\AppData\Roaming\Thunderbird\Profiles\abc.default`
	calls, closed := fakeCreateProcess(t, nil)
	e := &winEnv{w: fakeWin32{}, clock: time.Now}
	if err := e.startHidden(tbExe, prof); err != nil {
		t.Fatalf("startHidden: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("%d CreateProcess requests, want 1", len(*calls))
	}
	c, want := (*calls)[0], buildLaunchCmd(tbExe, prof)
	if c.app != want.app || c.cmdLine != want.cmdLine || c.dir != want.dir || c.inherit || c.flags != want.flags {
		t.Errorf("request %+v, want the built start %+v", c, want)
	}
	if c.si.Cb != uint32(unsafe.Sizeof(windows.StartupInfo{})) || c.si.Flags != windows.STARTF_USESHOWWINDOW || c.si.ShowWindow != windows.SW_SHOWMINNOACTIVE {
		t.Errorf("STARTUPINFO cb %d flags %#x show %d, want its size, STARTF_USESHOWWINDOW only, SW_SHOWMINNOACTIVE", c.si.Cb, c.si.Flags, c.si.ShowWindow)
	}
	if c.si.StdInput != 0 || c.si.StdOutput != 0 || c.si.StdErr != 0 {
		t.Errorf("STARTUPINFO hands Thunderbird standard handles: %+v", c.si)
	}
	n := map[windows.Handle]int{}
	for _, h := range *closed {
		n[h]++
	}
	if len(*closed) != 2 || n[0x111] != 1 || n[0x222] != 1 {
		t.Errorf("closed %v, want the thread handle 0x222 and the process handle 0x111 once each", *closed)
	}

	calls, closed = fakeCreateProcess(t, windows.ERROR_FILE_NOT_FOUND)
	if err := e.startHidden(tbExe, prof); err == nil || len(*calls) != 1 || len(*closed) != 0 {
		t.Errorf("a refused start: err %v, %d requests, closed %v; want an error, 1 request, nothing closed", err, len(*calls), *closed)
	}
}

// covers: MA-923
// The command line is quoted byte for byte as Go's exec quotes it — winEscapeArg
// equals syscall.EscapeArg on every string of up to four characters drawn from a
// letter, a backslash, a quote, a space and a tab — and Windows' own split
// (CommandLineToArgvW, what the session's -profile identity uses) gives each
// argument back whole. The Win32 values the untagged builder names equal x/sys's.
func TestWinEscapeArgMatchesGo(t *testing.T) {
	alphabet := []string{"a", `\`, `"`, " ", "\t"}
	cases := []string{""}
	for n, prev := 0, []string{""}; n < 4; n++ {
		var next []string
		for _, p := range prev {
			for _, c := range alphabet {
				next = append(next, p+c)
			}
		}
		cases, prev = append(cases, next...), next
	}
	cases = append(cases, `C:\Users\First Last\p\`, `C:\x y\a"b\`, `C:\p" -foo "x`)
	for _, s := range cases {
		if got, want := winEscapeArg(s), syscall.EscapeArg(s); got != want {
			t.Errorf("winEscapeArg(%q) = %q, Go's EscapeArg = %q", s, got, want)
		}
		args, err := windows.DecomposeCommandLine(winCommandLine(tbExe, "-profile", s))
		if err != nil || len(args) != 3 || args[0] != tbExe || args[1] != "-profile" || args[2] != s {
			t.Errorf("profile %q came back as %q (%v)", s, args, err)
		}
	}
	if wCreateBreakawayFromJob != windows.CREATE_BREAKAWAY_FROM_JOB || wStartfUseShowWindow != windows.STARTF_USESHOWWINDOW || swShowMinNoActive != windows.SW_SHOWMINNOACTIVE {
		t.Error("the builder's Win32 values differ from x/sys's")
	}
}

// covers: MA-923
// A real start: startHidden starts a stand-in named thunderbird.exe in a folder of
// its own (a copy of this test binary, which writes what it received and exits).
// Windows gives it exactly `<exe> -profile <profile>` — the profile one argument
// whatever its spaces, quotes or trailing backslash, by Windows' split and Go's —
// the stand-in's own folder as its working folder, and a STARTUPINFO asking for
// SW_SHOWMINNOACTIVE. Nothing is waited on: the report is polled for.
func TestWinStartRealProcess(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Mozilla Thunderbird")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "thunderbird.exe")
	copyFile(t, self, exe)

	for i, prof := range []string{
		`C:\p`,
		`C:\Users\First Last\AppData\Roaming\Thunderbird\Profiles\abc.default`,
		`C:\Users\First Last\p\`,
		`C:\x y\a"b\`,
	} {
		out := filepath.Join(t.TempDir(), "report.json")
		t.Setenv(startHelperEnv, out)
		e := &winEnv{w: fakeWin32{}, clock: time.Now}
		if err := e.startHidden(exe, prof); err != nil {
			t.Fatalf("case %d: startHidden: %v", i, err)
		}
		r := waitReport(t, out)
		want := []string{exe, "-profile", prof}
		if !equalArgs(r.Argv, want) || !equalArgs(r.Args, want) {
			t.Errorf("case %d: Windows' split %q, Go's %q; want %q", i, r.Argv, r.Args, want)
		}
		if r.Raw != buildLaunchCmd(exe, prof).cmdLine {
			t.Errorf("case %d: command line %q, want the built %q", i, r.Raw, buildLaunchCmd(exe, prof).cmdLine)
		}
		if !eqFoldPath(r.Dir, dir) {
			t.Errorf("case %d: working folder %q, want the install folder %q", i, r.Dir, dir)
		}
		if r.Flags&windows.STARTF_USESHOWWINDOW == 0 || r.Show != windows.SW_SHOWMINNOACTIVE {
			t.Errorf("case %d: STARTUPINFO flags %#x show %d, want STARTF_USESHOWWINDOW with SW_SHOWMINNOACTIVE", i, r.Flags, r.Show)
		}
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	src, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
}

// waitReport polls for the stand-in's report for up to a minute (the lab VM runs
// amd64 programs emulated).
func waitReport(t *testing.T, path string) startReport {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			var r startReport
			if err := json.Unmarshal(b, &r); err != nil {
				t.Fatalf("the stand-in's report: %v", err)
			}
			return r
		}
		if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			t.Fatalf("no report from the stand-in: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// eqFoldPath compares two folders as Windows does (case-insensitive, either
// trailing separator ignored).
func eqFoldPath(a, b string) bool {
	return eqFold(filepath.Clean(a), filepath.Clean(b))
}
