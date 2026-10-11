package instance

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

func sampleInfo(t *testing.T) Info {
	t.Helper()
	k, _ := NewKey()
	return Info{PID: os.Getpid(), Exe: "/x/mailarchive-desktop", Started: time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC),
		Config: "/c/graph-config.json", Dashboard: "127.0.0.1:53210", Reader: "127.0.0.1:53211", Key: k}
}

// covers: MA-376, R23, S43
// The instance file round-trips, is written 0600 into a 0700 directory, and a
// file that cannot be trusted reads as unusable — never believed, never repaired:
// a missing file is ErrNotExist; malformed JSON, an unknown version, a missing
// address, a key that is not 32 bytes of hex, an oversized file, a link, and (on
// Unix) a file others can read are all ErrUnusable [CC21, §8.2].
func TestInstanceFileIsReadOnlyWhenTrustworthy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, FileName)
	in := sampleInfo(t)
	if err := Write(path, in); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("a healthy instance file was refused: %v", err)
	}
	if got.PID != in.PID || got.Dashboard != in.Dashboard || got.Key != in.Key || got.V != 1 || got.Config != in.Config {
		t.Errorf("round trip: %+v, want %+v", got, in)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("instance file mode %o, want 600", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
			t.Errorf("state directory mode %o, want 700", fi.Mode().Perm())
		}
	}
	if _, err := Read(filepath.Join(dir, "absent.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing file: %v, want ErrNotExist", err)
	}

	bad := map[string]string{
		"malformed":       `{"v":1,`,
		"unknown version": `{"v":2,"pid":1,"dashboard":"127.0.0.1:1","key":"` + in.Key + `"}`,
		"no address":      `{"v":1,"pid":1,"key":"` + in.Key + `"}`,
		"short key":       `{"v":1,"pid":1,"dashboard":"127.0.0.1:1","key":"abcd"}`,
		"key not hex":     `{"v":1,"pid":1,"dashboard":"127.0.0.1:1","key":"` + strings.Repeat("zz", 32) + `"}`,
		"oversized":       `{"v":1,"pad":"` + strings.Repeat("x", 9<<10) + `"}`,
	}
	for name, body := range bad {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(p); !errors.Is(err, ErrUnusable) {
			t.Errorf("%s: %v, want ErrUnusable", name, err)
		}
	}
	if runtime.GOOS != "windows" {
		wide := filepath.Join(dir, "wide.json")
		if err := os.WriteFile(wide, []byte(`{"v":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(wide); !errors.Is(err, ErrUnusable) || !strings.Contains(err.Error(), "chmod 600") {
			t.Errorf("a world-readable instance file: %v, want ErrUnusable naming chmod 600", err)
		}
		link := filepath.Join(dir, "link.json")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(link); !errors.Is(err, ErrUnusable) {
			t.Errorf("a symlink: %v, want ErrUnusable", err)
		}
	}
}

// covers: MA-376, R23
// The owner check runs on every OS with an injected reader (the Windows one reads a
// SID): a file this account owns passes, an elevated process's Administrators-
// owned file passes (the token's default owner), another account's file is
// refused naming the owner, and an owner that cannot be read refuses [CC21].
func TestInstanceFileOwnerIsChecked(t *testing.T) {
	mine, admins, other := "S-1-5-21-111-222-333-1001", "S-1-5-32-544", "S-1-5-21-111-222-333-1002"
	read := func(owner string, err error) func(string) (string, []string, error) {
		return func(string) (string, []string, error) { return owner, []string{mine, admins}, err }
	}
	for _, c := range []struct {
		name  string
		owner string
		err   error
		ok    bool
	}{
		{"this account's user SID", mine, nil, true},
		{"the token's default owner (elevated)", admins, nil, true},
		{"another account", other, nil, false},
		{"unreadable owner", "", errors.New("access denied"), false},
	} {
		err := CheckOwner(`C:\Users\a\AppData\Local\MailArchiveDesktop\desktop-instance.json`, read(c.owner, c.err))
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
		if err != nil && c.owner != "" && !strings.Contains(err.Error(), other) {
			t.Errorf("%s: the refusal does not name the owner %s: %v", c.name, other, err)
		}
	}
	// Read goes through the same check: an injected reader that says "someone else".
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := Write(path, sampleInfo(t)); err != nil {
		t.Fatal(err)
	}
	was := OwnerReader
	t.Cleanup(func() { OwnerReader = was })
	OwnerReader = read(other, nil)
	if _, err := Read(path); !errors.Is(err, ErrUnusable) {
		t.Errorf("an instance file owned by another account: %v, want ErrUnusable", err)
	}
	OwnerReader = read(mine, nil)
	if _, err := Read(path); err != nil {
		t.Errorf("an instance file owned by this account was refused: %v", err)
	}
}

// covers: MA-376, R12
// With no per-account folder to be found (no HOME, no %LOCALAPPDATA%) the launch
// has nowhere trustworthy to keep its lock and instance file and refuses, rather
// than guess one [CC21]; the folder is the product's config directory (never the
// cache directory a cleaner purges), or %LOCALAPPDATA%\MailArchiveDesktop.
func TestDirFailsClosed(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("AppData", "")
	_, err := Dir()
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1), assure.Names("cannot determine your per-account folder"))

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "Local"))
	d, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if d != filepath.Join(home, "Local", "MailArchiveDesktop") {
			t.Errorf("Dir() = %s, want %%LOCALAPPDATA%%\\MailArchiveDesktop", d)
		}
	} else if filepath.Base(d) != "mailarchive" || strings.Contains(strings.ToLower(d), "cache") {
		t.Errorf("Dir() = %s, want the product's config directory (…/mailarchive), not a cache directory", d)
	}
	if a, b := ServeFileName("/some/where/archive"), ServeFileName("/some/where/other"); a == b || !strings.HasPrefix(a, "serve-") || !strings.HasSuffix(a, ".json") {
		t.Errorf("ServeFileName must differ per archive: %q, %q", a, b)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// covers: MA-376, R23
// The lock and the instance file: a second Acquire is refused (ErrHeld) and the
// holder's pid is readable for the refusal text; Publish writes the file; a stale
// file is removed right after the lock is taken; the watch rewrites a file that
// has gone missing while the lock is held; Close removes the file and releases the
// lock so the next launch is the instance [CC21].
func TestHolderLockAndInstanceFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	h, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir); !IsHeld(err) {
		t.Fatalf("a second Acquire: %v, want the lock held", err)
	}
	if runtime.GOOS != "windows" { // a Windows byte-range lock hides its holder line from other readers
		if got := HolderPID(dir); got != os.Getpid() {
			t.Errorf("HolderPID = %d, want %d", got, os.Getpid())
		}
	}

	stale := h.FilePath()
	if err := os.WriteFile(stale, []byte("stale from a crashed run"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.RemoveStale()
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Errorf("the stale instance file survived RemoveStale: %v", err)
	}
	if err := h.Publish(sampleInfo(t)); err != nil {
		t.Fatal(err)
	}
	var logged []string
	stop := h.Watch(10*time.Millisecond, func(s string) { logged = append(logged, s) })
	os.Remove(h.FilePath())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Read(h.FilePath()); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := Read(h.FilePath()); err != nil {
		t.Errorf("the deleted instance file was not rewritten by the lock holder: %v (log %v)", err, logged)
	}
	stop()

	h.Close()
	if _, err := os.Lstat(h.FilePath()); !os.IsNotExist(err) {
		t.Errorf("Close left the instance file behind")
	}
	again, err := Acquire(dir)
	if err != nil {
		t.Fatalf("the lock is not free after Close: %v", err)
	}
	again.Close()
}
