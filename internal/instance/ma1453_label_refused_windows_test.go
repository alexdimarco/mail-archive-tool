//go:build windows

package instance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/util"
)

// lowWriteEnv, when set, makes TestMA1453_WindowsRefusingTheLabelStillLeavesAUsableFile the child
// it starts at Low integrity: it writes the instance file the variable names (WriteNoting) and
// prints the outcome on one line.
const lowWriteEnv = "MA1453_LOW_WRITE"

// lowLabelled makes dir writable at Low integrity: a Low mandatory label, inherited by what is
// made in it, so a Low process may create files there (as in a browser's own cache folder).
func lowLabelled(t *testing.T, dir string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("S:(ML;OICI;NW;;;LW)")
	if err != nil {
		t.Fatal(err)
	}
	sacl, _, err := sd.SACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, sacl); err != nil {
		t.Fatalf("labelling %s Low: %v", dir, err)
	}
}

// writeAtLow runs this test binary as a child under a duplicate of this process's token lowered to
// Low integrity; the child writes the instance file at path and answers one line.
func writeAtLow(t *testing.T, path string) string {
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
	c := exec.Command(os.Args[0], "-test.run=^TestMA1453_WindowsRefusingTheLabelStillLeavesAUsableFile$", "-test.count=1")
	c.Env = append(os.Environ(), lowWriteEnv+"="+path)
	c.SysProcAttr = &syscall.SysProcAttr{Token: syscall.Token(dup)}
	out, err := c.CombinedOutput()
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "LOWWRITE ") {
			return strings.TrimSpace(l)
		}
	}
	t.Fatalf("the Low-integrity child gave no answer (%v): %s", err, out)
	return ""
}

// covers: MA-1453, R23
// A real refusal of the label by Windows, not a stand-in: a program may not give a file a label
// above its own integrity, so a Low-integrity child of this account, writing its instance file in
// a folder it may write, is refused the Medium label. The refusal is the error the fallback knows
// (util.SecretCreateRefused), and the file is written as 1.7.1 wrote it, readable back as an
// instance file, with the plain warning given: the program that wrote it starts. Positive twin
// first: this process, at its own integrity, writes the file with the label and is told nothing.
// (Whether a person's own Medium program may set a Medium label, the research note's L0, needs a
// signed-in person; this lab runs as SYSTEM.)
func TestMA1453_WindowsRefusingTheLabelStillLeavesAUsableFile(t *testing.T) {
	if p := os.Getenv(lowWriteEnv); p != "" {
		keyHex, _ := NewKey()
		var note string
		raw := writeSecret(p+".probe", []byte("x"))
		_, _, refused := util.SecretCreateRefused(raw)
		err := WriteNoting(p, Info{PID: os.Getpid(), Dashboard: "127.0.0.1:1", Key: keyHex}, func(s string) { note = s })
		got, rerr := Read(p)
		fmt.Printf("LOWWRITE integrity=%s labelrefused=%v err=%v read=%v keyok=%v note=%q\n", integrityOf(), refused, err, rerr, rerr == nil && got.Key == keyHex, note)
		return
	}
	dir := t.TempDir()
	keyHex, _ := NewKey()
	var said []string
	twin := filepath.Join(dir, "medium", FileName)
	if err := WriteNoting(twin, Info{PID: os.Getpid(), Dashboard: "127.0.0.1:1", Key: keyHex}, func(s string) { said = append(said, s) }); err != nil || len(said) != 0 || !noReadUp(labelOf(t, twin)) {
		t.Fatalf("twin: at this process's integrity the write gave %v, said %q, label %q", err, said, labelOf(t, twin))
	}
	lowDir := filepath.Join(dir, "low")
	if err := os.MkdirAll(lowDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lowLabelled(t, lowDir)
	path := filepath.Join(lowDir, FileName)
	got := writeAtLow(t, path)
	t.Logf("the Low child: %s; the file's label: %s", got, labelOf(t, path))
	for _, want := range []string{"integrity=S-1-16-4096", "labelrefused=true", "err=<nil>", "read=<nil>", "keyok=true", `note="warning: Windows would not protect MailArchive Desktop's private start file in `} {
		if !strings.Contains(got, want) {
			t.Errorf("the Low child's write: %s, want %s", got, want)
		}
	}
	if noReadUp(labelOf(t, path)) {
		t.Errorf("the Low child's file carries the Medium label it was refused: %s", labelOf(t, path))
	}
}
