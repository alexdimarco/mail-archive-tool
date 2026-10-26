package keyring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-782
// A password file is read whole, one line ending stripped, and opens the archive;
// a recovery-key file likewise. Refused, naming the file and the fix: readable by
// group or others, a link, a folder, empty, over 4 KiB.
func TestASecretFileIsReadOnlyWhenItIsTheOwnersAlone(t *testing.T) {
	freshRecord(t)
	out, rk, _ := made(t, nil)
	dir := t.TempDir()
	write := func(name, body string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}

	for name, body := range map[string]string{"lf": testPassword + "\n", "crlf": testPassword + "\r\n", "bare": testPassword} {
		s, err := PasswordFile(write("pw-"+name, body, 0o600))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		u, err := Open(out, s, OpenOptions{})
		if err != nil {
			t.Fatalf("%s: the password read from the file does not open the archive: %v", name, err)
		}
		u.Destroy()
	}
	s, err := RecoveryKeyFile(write("rk", rk+"\n", 0o600))
	if err != nil {
		t.Fatal(err)
	}
	u, err := Open(out, s, OpenOptions{})
	assure.Reached(t, u, "the archive opened by a recovery-key file")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	if got := fmt.Sprintf("%v %+v %#v %s", s, s, s, s); strings.Contains(got, rk) {
		t.Fatalf("a secret printed its text: %s", got)
	}

	if err := os.Symlink(filepath.Join(dir, "pw-lf"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		path  string
		names []string
	}{
		"a link":      {filepath.Join(dir, "link"), []string{"plain file"}},
		"a folder":    {dir, []string{"plain file"}},
		"empty":       {write("empty", "\n", 0o600), []string{"empty"}},
		"over 4 KiB":  {write("big", strings.Repeat("x", maxSecretFile+1), 0o600), []string{"larger than"}},
		"not present": {filepath.Join(dir, "absent"), []string{"cannot be read"}},
	}
	if runtime.GOOS != "windows" {
		cases["readable by group"] = struct {
			path  string
			names []string
		}{write("group", testPassword, 0o640), []string{"chmod 600"}}
		cases["readable by others"] = struct {
			path  string
			names []string
		}{write("others", testPassword, 0o604), []string{"chmod 600"}}
	}
	for name, c := range cases {
		s, err := PasswordFile(c.path)
		code, msg := refusal(err)
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(append([]string{c.path}, c.names...)...),
			assure.Forbid(testPassword), assure.NoSideEffect(func() bool { return s.text == "" }))
		if !errors.Is(err, ErrSecretFile) {
			t.Fatalf("%s: want ErrSecretFile, got %v", name, err)
		}
	}
}
