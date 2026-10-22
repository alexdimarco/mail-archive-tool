package presync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenHygieneTokens must never appear in this package's non-test source: a
// PATH lookup, any kill or signal of a process, a Windows taskkill, or a shell
// interpreter path. The pre-sync starts only fixed OS tools by absolute path
// (MA-612) and NEVER signals or kills Thunderbird (R31); its own helpers end at
// a context deadline, which the Go runtime enforces — there is no Kill/Signal in
// our code. (Comments are written to avoid these exact tokens so the text walk
// stays simple and robust, the MA-621 idiom.)
var forbiddenHygieneTokens = []string{
	"LookPath",
	"SIGKILL", "SIGTERM", "syscall.Kill",
	".Kill(", ".Signal(",
	"taskkill",
	"/bin/sh", "/bin/bash", "/bin/zsh", "/bin/dash",
	// Windows kill primitives (PC-W7): the Windows adapter asks Thunderbird to
	// quit with PostMessageW(WM_CLOSE) and must NEVER force-terminate it or a
	// window. "PROCESS_TERMINATE" catches an OpenProcess opened for a kill without
	// flagging benign OpenProcess(QUERY_LIMITED/SYNCHRONIZE); the window messages
	// are the forceful alternatives to WM_CLOSE.
	"TerminateProcess", "PROCESS_TERMINATE",
	"WM_QUIT", "WM_DESTROY", "WM_ENDSESSION",
	"EndTask", "ExitWindowsEx", "PostQuitMessage",
	"HWND_BROADCAST", "PostThreadMessage",
}

func scanForbidden(src string, forbidden []string) []string {
	var hits []string
	for _, tok := range forbidden {
		if strings.Contains(src, tok) {
			hits = append(hits, tok)
		}
	}
	return hits
}

// covers: MA-557
// Exec and signal hygiene (design §4.7.1, §5.4, invariant R31): a static text
// walk of internal/presync's non-test source finds none of the forbidden tokens,
// so the pre-sync can never resolve a name on PATH, launch a shell, or
// kill/signal another process. The fail-closed twin confirms the scanner catches
// each planted pattern; the argv-catalog half of MA-557 is TestMacCommandArgvGolden,
// and that every exec site names a closed-set OS tool is MA-612.
func TestPresyncExecSignalHygiene(t *testing.T) {
	dir, err := os.Getwd() // the package directory when `go test` runs it
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	scanned := 0
	var hits []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		scanned++
		for _, tok := range scanForbidden(string(data), forbiddenHygieneTokens) {
			hits = append(hits, name+": "+tok)
		}
	}
	if scanned == 0 {
		t.Fatal("no non-test source scanned — the walk is not reading the package")
	}
	if len(hits) > 0 {
		t.Errorf("forbidden exec/signal token(s) in internal/presync (R31/§5.4): %v", hits)
	}

	// Fail-closed twin: planted Unix AND Windows kill primitives are caught.
	twin := `exec.LookPath("x"); p.Kill(); p.Signal(os.Interrupt); exec.Command("/bin/sh", "-c", s); ` +
		`windows.TerminateProcess(h, 1); windows.OpenProcess(PROCESS_TERMINATE, ...); PostMessageW(hwnd, WM_QUIT, 0, 0)`
	caught := scanForbidden(twin, forbiddenHygieneTokens)
	for _, want := range []string{"LookPath", ".Kill(", ".Signal(", "/bin/sh", "TerminateProcess", "PROCESS_TERMINATE", "WM_QUIT"} {
		if !containsStrHyg(caught, want) {
			t.Errorf("twin: forbidden token %q was not caught (caught %v)", want, caught)
		}
	}
}

func containsStrHyg(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
