//go:build windows

package instance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/util"
)

// lowReadEnv, when set, makes TestMA1427_ALowerIntegrityProcessCannotReadTheInstanceFile the
// child it starts at Low integrity: it reads the file the variable names and prints the
// outcome on one line.
const lowReadEnv = "MA1427_LOW_READ"

// mediumNoReadUp matches a Medium mandatory label in an SDDL string and captures its rights.
var mediumNoReadUp = regexp.MustCompile(`\(ML;[^;]*;([A-Z]*);;;ME\)`)

// labelOf is the mandatory label of path as SDDL ("" when it has none).
func labelOf(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("reading the label of %s: %v", path, err)
	}
	return sd.String()
}

// noReadUp reports whether sddl holds a Medium label forbidding read, write and execute up.
func noReadUp(sddl string) bool {
	m := mediumNoReadUp.FindStringSubmatch(sddl)
	if m == nil {
		return false
	}
	for _, r := range []string{"NR", "NW", "NX"} {
		if !strings.Contains(m[1], r) {
			return false
		}
	}
	return true
}

// integrityOf is the integrity level SID of this process's token.
func integrityOf() string {
	tok := windows.GetCurrentProcessToken()
	var n uint32
	_ = windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, nil, 0, &n)
	if n == 0 {
		return "unknown"
	}
	b := make([]byte, n)
	if err := windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, &b[0], n, &n); err != nil {
		return "unknown"
	}
	return (*windows.Tokenmandatorylabel)(unsafe.Pointer(&b[0])).Label.Sid.String()
}

// readAtLow runs this test binary as a child under a duplicate of this process's token
// lowered to Low integrity (S-1-16-4096, what a browser-style sandbox runs at), which reads
// path and answers one line.
func readAtLow(t *testing.T, path string) string {
	t.Helper()
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT, &tok); err != nil {
		t.Fatalf("opening this process's token: %v", err)
	}
	defer tok.Close()
	var dup windows.Token
	if err := windows.DuplicateTokenEx(tok, windows.MAXIMUM_ALLOWED, nil, windows.SecurityImpersonation, windows.TokenPrimary, &dup); err != nil {
		t.Fatalf("duplicating the token: %v", err)
	}
	defer dup.Close()
	low, err := windows.CreateWellKnownSid(windows.WinLowLabelSid)
	if err != nil {
		t.Fatal(err)
	}
	tml := windows.Tokenmandatorylabel{Label: windows.SIDAndAttributes{Sid: low, Attributes: windows.SE_GROUP_INTEGRITY}}
	if err := windows.SetTokenInformation(dup, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&tml)), tml.Size()); err != nil {
		t.Fatalf("lowering the token to Low integrity: %v", err)
	}
	c := exec.Command(os.Args[0], "-test.run=^TestMA1427_ALowerIntegrityProcessCannotReadTheInstanceFile$", "-test.count=1")
	c.Env = append(os.Environ(), lowReadEnv+"="+path)
	c.SysProcAttr = &syscall.SysProcAttr{Token: syscall.Token(dup)}
	out, err := c.CombinedOutput()
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "LOWREAD ") {
			return strings.TrimSpace(l)
		}
	}
	t.Fatalf("the Low-integrity child gave no answer (%v): %s", err, out)
	return ""
}

// covers: MA-1427, R23
// The instance file's key K yields a Desktop session through the hand-off, so on Windows a
// Low-integrity process of the same account (a browser-style sandbox) must not read it
// (the menu-bar S1 adversarial re-check r3, C1). Positive twins first: this process, at its
// own integrity, reads the file Write made, and a Low-integrity child of the same account
// reads a file written without the label (so the child can read what the DACL allows, and
// a refusal below is the label's doing). Then: the file carries a Medium label with
// no-read-up, no-write-up and no-execute-up; the temporary file had it before it was renamed
// into place (it is created with it, never set by a later step); and the Low child cannot
// open it, nor read it as an instance file.
func TestMA1427_ALowerIntegrityProcessCannotReadTheInstanceFile(t *testing.T) {
	if p := os.Getenv(lowReadEnv); p != "" {
		il := integrityOf()
		if il != "S-1-16-4096" {
			fmt.Printf("LOWREAD not-low integrity=%s\n", il)
			return
		}
		f, err := os.Open(p)
		if err != nil {
			fmt.Printf("LOWREAD denied integrity=%s err=%v\n", il, err)
			return
		}
		f.Close()
		if _, err := Read(p); err != nil {
			fmt.Printf("LOWREAD opened-not-read integrity=%s err=%v\n", il, err)
			return
		}
		fmt.Printf("LOWREAD read integrity=%s\n", il)
		return
	}
	dir := t.TempDir()
	keyHex, _ := NewKey()
	info := Info{PID: os.Getpid(), Dashboard: "127.0.0.1:1", Key: keyHex, Acts: KnownActs}

	plain := filepath.Join(dir, "unlabelled.json")
	if err := util.WriteFileAtomic0600(plain, []byte(`{"v":1,"pid":1,"dashboard":"127.0.0.1:1","key":"`+keyHex+`"}`+"\n")); err != nil {
		t.Fatal(err)
	}
	if got := readAtLow(t, plain); !strings.HasPrefix(got, "LOWREAD read ") {
		t.Fatalf("twin: a Low-integrity child could not read a file without the label (%s): the probe proves nothing", got)
	}

	var tmpLabel string
	orig := util.BeforeAtomicRename
	t.Cleanup(func() { util.BeforeAtomicRename = orig })
	util.BeforeAtomicRename = func(tmp string) { tmpLabel = labelOf(t, tmp) }
	path := FilePathIn(dir, "1")
	if err := Write(path, info); err != nil {
		t.Fatal(err)
	}
	util.BeforeAtomicRename = orig
	if got, err := Read(path); err != nil || got.Key != keyHex {
		t.Fatalf("twin: this process could not read the instance file it wrote: %v", err)
	}
	if l := labelOf(t, path); !noReadUp(l) {
		t.Errorf("the instance file's label is %q, want a Medium label with NR, NW and NX", l)
	}
	if !noReadUp(tmpLabel) {
		t.Errorf("the temporary file's label before its rename was %q: the label was not set at creation", tmpLabel)
	}
	got := readAtLow(t, path)
	if !strings.HasPrefix(got, "LOWREAD denied ") {
		t.Errorf("a Low-integrity process of this account reached the instance file: %s", got)
	}
	t.Logf("this process at %s; the file's label %s; the Low child: %s", integrityOf(), labelOf(t, path), got)
}
