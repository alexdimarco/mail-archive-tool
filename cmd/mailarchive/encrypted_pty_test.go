//go:build linux || darwin

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tty/ptytest"
)

// rawPty is everything a pty's master end shows, prompts without a newline
// included.
type rawPty struct {
	t      *testing.T
	master *os.File
	mu     sync.Mutex
	buf    bytes.Buffer
}

func readPty(t *testing.T, master *os.File) *rawPty {
	p := &rawPty{t: t, master: master}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := master.Read(b)
			p.mu.Lock()
			p.buf.Write(b[:n])
			p.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return p
}

func (p *rawPty) shown() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

// await waits for the n-th appearance of text, failing after d.
func (p *rawPty) await(text string, n int, d time.Duration) {
	p.t.Helper()
	deadline := time.Now().Add(d)
	for strings.Count(p.shown(), text) < n {
		if time.Now().After(deadline) {
			p.t.Fatalf("waited for %q (#%d); the terminal shows:\n%q", text, n, p.shown())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (p *rawPty) typeIn(s string) {
	p.t.Helper()
	if _, err := p.master.Write([]byte(s)); err != nil {
		p.t.Fatal(err)
	}
}

// runOnPty starts the CLI on a fresh pseudo-terminal with env.
func runOnPty(t *testing.T, env []string, args ...string) (*rawPty, chan error) {
	t.Helper()
	master, slave := ptytest.Open(t)
	p := readPty(t, master)
	cmd := ptytest.Command(slave, testBin, args...)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { cmd.Process.Kill() })
	return p, done
}

func exitOf(t *testing.T, done chan error, d time.Duration) int {
	t.Helper()
	select {
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	case <-time.After(d):
		t.Fatalf("the CLI did not finish within %v", d)
	}
	return -1
}

// covers: MA-786
// Ctrl-C at the new password's prompt ends -encrypt as an interrupt (exit 130),
// saying nothing was changed, with typing shown again and no key file made.
func TestCtrlCAtThePasswordPromptMakesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "archive")
	p, done := runOnPty(t, isolatedEnv(t), "-encrypt", "-out", out, "-input", "../../testdata/support.pst", "-remember=false")
	p.await("Choose a password: ", 1, 30*time.Second)
	p.typeIn("\x03")
	code := exitOf(t, done, 30*time.Second)
	_, err := os.Stat(filepath.Join(out, ".mailarchive-vault.json"))
	assure.Refused(t, code, p.shown(), assure.Code(130), assure.Names("interrupted", "nothing was changed"),
		assure.NoSideEffect(func() bool { return os.IsNotExist(err) }))
}

var reRecoveryKey = regexp.MustCompile(`[A-Z2-7]{4}(?:-[A-Z2-7]{4}){5}-[A-Z2-7]{3}`)

// covers: MA-786, MA-787, R37
// The real program on a real terminal: `-encrypt` asks for the password twice with
// typing hidden, asks whether to remember the key, shows the recovery key on a screen
// of its own and takes it away, asks for it again, then makes the archive and runs
// its first backup — sealed, recorded. A later backup without a terminal is refused,
// naming why; at the terminal it asks for the password and runs.
func TestTheCLIMakesAndBacksUpAnEncryptedArchiveAtARealTerminal(t *testing.T) {
	env := isolatedEnv(t)
	out := filepath.Join(t.TempDir(), "archive")
	const pw = "pty test passphrase"

	p, done := runOnPty(t, env, "-encrypt", "-out", out, "-input", "../../testdata/support.pst", "-remember=false")
	p.await("Choose a password: ", 1, 30*time.Second)
	p.typeIn(pw + "\n")
	p.await("Type it again: ", 1, 10*time.Second)
	p.typeIn(pw + "\n")
	p.await("Press Enter once you have saved it.", 1, 60*time.Second)
	key := reRecoveryKey.FindString(p.shown())
	assure.Reached(t, key, "the recovery key shown")
	p.typeIn("\n")
	p.await("Type or paste the recovery key", 1, 10*time.Second)
	p.typeIn(key + "\n")
	if code := exitOf(t, done, 3*time.Minute); code != 0 {
		t.Fatalf("-encrypt exited %d:\n%q", code, p.shown())
	}
	screen := p.shown()
	if strings.Contains(screen, pw) {
		t.Fatal("the password was shown on the terminal")
	}
	// The key appears only on the alternate screen, which is taken away.
	enter, leave := strings.Index(screen, "\x1b[?1049h"), strings.Index(screen, "\x1b[?1049l")
	if enter < 0 || leave < enter || strings.Contains(screen[:enter], key) || strings.Contains(screen[leave:], key) {
		t.Fatalf("the recovery key was shown outside a screen of its own:\n%q", screen)
	}
	for _, name := range []string{".mailarchive-vault.json", "units"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("the archive lacks %s: %v", name, err)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(out, "*", "*.html")); len(m) > 0 {
		t.Fatalf("an encrypted backup wrote plaintext pages: %v", m)
	}

	// No terminal: refused, naming why.
	cmd := exec.Command(testBin, "-out", out, "-input", "../../testdata/support.pst")
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	assure.Refused(t, code, stderr.String(), assure.Code(1), assure.Names(out, "terminal"))

	// At the terminal: the password, then the backup.
	p, done = runOnPty(t, env, "-out", out, "-input", "../../testdata/support.pst")
	p.await("Password (or recovery key) for the encrypted archive", 1, 30*time.Second)
	p.typeIn(pw + "\n")
	if code := exitOf(t, done, 3*time.Minute); code != 0 {
		t.Fatalf("the keyed backup exited %d:\n%q", code, p.shown())
	}
	if strings.Contains(p.shown(), pw) {
		t.Fatal("the password was shown on the terminal")
	}
}
