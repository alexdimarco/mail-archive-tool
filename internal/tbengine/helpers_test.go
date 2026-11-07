package tbengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/tbrestore"
	"mail-archive-tool/internal/thunderbird"
)

// Fixtures: real archives made by a real backup of a Thunderbird-style folder of mbox
// files (with its originals kept), an encrypted one through its key, and fake
// Thunderbird profiles in temp folders. Nothing of the engine is mocked; the seams are
// the OS probes (process list, elevation, Store, network, free space) and the writer's
// file system, wrapped for fault injection.

// canary is in every message's body: a scan for it finds plaintext wherever it is.
const canary = "PLAINTEXT-CANARY-7f3a91"

// The source's folders, named so no other path or word holds them: a scan of
// restores.log or the progress for a folder name finds only these.
const (
	folderA = "Zebrafolder"
	folderB = "Quokkasub" // inside folderA
	folderC = "Sentbox"
)

func mboxOf(subjects ...string) string {
	var b strings.Builder
	for i, s := range subjects {
		b.WriteString("From - Mon Jan 0" + string(rune('1'+i)) + " 00:00:00 2024\r\n")
		b.WriteString("From: Ann <ann@example.org>\r\nTo: bob@example.org\r\nSubject: " + s + "\r\n")
		b.WriteString("Date: Mon, 0" + string(rune('1'+i)) + " Jan 2024 10:00:00 +0000\r\nMessage-ID: <" + strings.ReplaceAll(s, " ", ".") + "@example.org>\r\n\r\n")
		// The source is a real mbox, so its body line "From here" is quoted; the archive
		// keeps it unquoted, and the restore quotes it again (mboxrd).
		b.WriteString("Body of " + s + " " + canary + "\r\n>From here a line\r\n\r\n")
	}
	return b.String()
}

// sourceDir is a Thunderbird-style folder: Zebrafolder (two messages) with Quokkasub
// below it (one), and Sentbox (one).
func sourceDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Mail")
	write(t, filepath.Join(dir, folderA), mboxOf("alpha one", "alpha two"))
	write(t, filepath.Join(dir, folderA+".sbd", folderB), mboxOf("beta one"))
	write(t, filepath.Join(dir, folderC), mboxOf("gamma one"))
	return dir
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

var quiet = log.New(io.Discard, "", 0)

// plainArchive backs the source up into a new plain archive, keeping originals when raw.
func plainArchive(t *testing.T, raw bool) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "Work mail")
	backup(t, sourceDir(t), out, raw, nil)
	return out
}

func backup(t *testing.T, src, out string, raw bool, u *keyring.Unlocked) {
	t.Helper()
	opts := app.Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: raw}
	if u != nil {
		done, err := keyring.Apply(&opts, u)
		if err != nil {
			t.Fatal(err)
		}
		defer done()
	}
	if _, err := app.Run(context.Background(), opts, quiet, nil); err != nil {
		t.Fatalf("backup: %v", err)
	}
}

const testPassword = "tbengine test passphrase"

// encryptedArchive backs the source up into a new encrypted archive and opens it again
// with its password, as a person unlocking it does.
func encryptedArchive(t *testing.T) (string, *keyring.Unlocked) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "Secret mail")
	pc, err := keyring.Create(out, testPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w, err := pc.Commit()
	if err != nil {
		t.Fatal(err)
	}
	backup(t, sourceDir(t), out, true, w)
	w.Destroy()
	u, err := keyring.Open(out, keyring.Password(testPassword), keyring.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(u.Destroy)
	return out, u
}

// profile is a fake Thunderbird profile: its folder, its Local Folders (holding
// Thunderbird's own Trash, which a restore must never touch).
type profile struct {
	dir, local string
}

func newProfile(t *testing.T, under string) profile {
	t.Helper()
	if under == "" {
		under = t.TempDir()
	}
	dir := filepath.Join(under, "p.default")
	write(t, filepath.Join(dir, "prefs.js"), "// Mozilla User Preferences\n"+strings.Join([]string{
		`user_pref("mail.accountmanager.localfoldersserver", "server1");`,
		`user_pref("mail.server.server1.directory-rel", "[ProfD]Mail/Local Folders");`,
		`user_pref("mail.server.server1.hostname", "Local Folders");`,
		`user_pref("mail.server.server1.type", "none");`,
		`user_pref("mail.server.server1.storeContractID", "@mozilla.org/msgstore/berkeleystore;1");`,
	}, "\n")+"\n")
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(dir, "Mail", "Local Folders")
	write(t, filepath.Join(local, "Trash"), "From - x\nSubject: old\n\nkept\n")
	write(t, filepath.Join(local, "Trash.msf"), "summary")
	dir, _ = filepath.EvalSymlinks(dir)
	local, _ = filepath.EvalSymlinks(local)
	return profile{dir: dir, local: local}
}

func (p profile) choice() ProfileChoice { return ProfileChoice{Dir: p.dir} }

// snapshot is every entry under each root (kind, and a file's SHA-256), sorted, and
// restores.log's bytes: what a refusal must leave as it found it. Thunderbird's own
// refusal counter (tbrestore.ThunderbirdRefusalFile) is left out.
func snapshot(t *testing.T, roots ...string) string {
	t.Helper()
	var lines []string
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Name() == tbrestore.ThunderbirdRefusalFile || d.Name() == lockfile.Name {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				tgt, _ := os.Readlink(p)
				lines = append(lines, "L "+root+" "+rel+" -> "+tgt)
			case d.IsDir():
				lines = append(lines, "D "+root+" "+rel)
			default:
				b, _ := os.ReadFile(p)
				s := sha256.Sum256(b)
				lines = append(lines, "F "+root+" "+rel+" "+hex.EncodeToString(s[:]))
			}
			return nil
		})
	}
	lp, _ := tbrestore.LogPath()
	b, _ := os.ReadFile(lp)
	s := sha256.Sum256(b)
	lines = append(lines, "LOG "+hex.EncodeToString(s[:]))
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// refusedAs asserts err is the restore's typed refusal code, its words naming each
// needle, with nothing changed since before.
func refusedAs(t *testing.T, err error, code string, unchanged func() bool, names ...string) {
	t.Helper()
	rc, msg := 0, ""
	if err != nil {
		msg = err.Error()
		if tbrestore.RefusalCode(err) == code {
			rc = 1
		}
	}
	if rc == 0 {
		t.Logf("want %s, got code %q: %v", code, tbrestore.RefusalCode(err), err)
	}
	opts := []assure.RefuseOption{assure.Code(1), assure.Names(names...)}
	if unchanged != nil {
		opts = append(opts, assure.NoSideEffect(unchanged))
	}
	assure.Refused(t, rc, msg, opts...)
}

// lockPaths are the three locks a run takes: the archive's, the MailArchive restore lock
// of the profile, and Thunderbird's profile lock.
func lockPaths(t *testing.T, out, profileDir string) []string {
	t.Helper()
	rl, err := thunderbird.RestoreLockPath(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	return []string{filepath.Join(out, lockfile.Name), rl, filepath.Join(profileDir, tbrestore.ParentLockName)}
}

// childTakes is what another process says when it tries each lock: "took" or "refused"
// per path, in order.
func childTakes(t *testing.T, paths []string) []string {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^$")
	c.Env = append(os.Environ(), "TBE_HELPER=locks", "TBE_PATHS="+strings.Join(paths, string(os.PathListSeparator)))
	b, err := c.Output()
	if err != nil {
		t.Fatalf("lock helper: %v", err)
	}
	return strings.Fields(strings.TrimSpace(string(b)))
}

// allFree asserts another process can take every one of the run's locks.
func allFree(t *testing.T, out, profileDir, when string) {
	t.Helper()
	got := childTakes(t, lockPaths(t, out, profileDir))
	if strings.Join(got, " ") != "took took took" {
		t.Fatalf("%s: another process takes the archive lock, the restore lock and Thunderbird's lock: %v", when, got)
	}
}

// plan is MakePlan, failing the test on a refusal.
func plan(t *testing.T, req Request) *Plan {
	t.Helper()
	p, err := MakePlan(req)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return p
}

// events are restores.log's events for the restores into a profile, in order.
func events(t *testing.T, profileDir string) []string {
	t.Helper()
	lp, _ := tbrestore.LogPath()
	lines, err := tbrestore.NewLog(lp).Read()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	var out []string
	for _, l := range lines {
		if l.Event == tbrestore.EvStarted && l.Profile == tbrestore.ProfileKeyOf(profileDir) {
			ids[l.ID] = true
		}
		if ids[l.ID] {
			out = append(out, l.Event)
		}
	}
	return out
}

// faultFS wraps the writer's file system: hook runs before every operation with its
// name and path; its error is the operation's; a panic in it stops the run as a crash.
type faultFS struct {
	inner      tbrestore.FS
	hook       func(op, name string) error
	privateErr error
}

func (f *faultFS) at(op, name string) error {
	if f.hook != nil {
		return f.hook(op, name)
	}
	return nil
}

func (f *faultFS) Mkdir(n string) error {
	if err := f.at("mkdir", n); err != nil {
		return err
	}
	return f.inner.Mkdir(n)
}

func (f *faultFS) Create(n string) (tbrestore.WFile, error) {
	if err := f.at("create", n); err != nil {
		return nil, err
	}
	w, err := f.inner.Create(n)
	if err != nil {
		return nil, err
	}
	return &faultFile{w: w, f: f, name: n}, nil
}

func (f *faultFS) Open(n string) (io.ReadCloser, error) {
	if err := f.at("open", n); err != nil {
		return nil, err
	}
	return f.inner.Open(n)
}

func (f *faultFS) Lstat(n string) (fs.FileInfo, error) { return f.inner.Lstat(n) }

func (f *faultFS) ReadDir(n string, fn func([]fs.DirEntry) bool) error {
	if err := f.at("readdir", n); err != nil {
		return err
	}
	return f.inner.ReadDir(n, fn)
}

func (f *faultFS) RemoveSeen(n string, seen map[string]fs.FileInfo) error {
	if err := f.at("remove", n); err != nil {
		return err
	}
	return f.inner.RemoveSeen(n, seen)
}

// Private passes through without the hook unless privateErr is set: making an entry
// owner-only is not one of the write's crash or fault steps (R6-01's own tests fail it).
func (f *faultFS) Private(n string, dir bool) error {
	if f.privateErr != nil {
		return f.privateErr
	}
	return f.inner.Private(n, dir)
}

func (f *faultFS) SyncDir(n string) error {
	if err := f.at("syncdir", n); err != nil {
		return err
	}
	return f.inner.SyncDir(n)
}

type faultFile struct {
	w    tbrestore.WFile
	f    *faultFS
	name string
}

func (w *faultFile) Write(p []byte) (int, error) {
	if err := w.f.at("write", w.name); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

func (w *faultFile) Sync() error {
	if err := w.f.at("sync", w.name); err != nil {
		return err
	}
	return w.w.Sync()
}

func (w *faultFile) Close() error { return w.w.Close() }

// withFS makes every run of the test write through hook (nil: the real root only).
func withFS(t *testing.T, hook func(op, name string) error) {
	t.Helper()
	was := fsFor
	fsFor = func(r *os.Root) tbrestore.FS { return &faultFS{inner: tbrestore.NewRootFS(r), hook: hook} }
	t.Cleanup(func() { fsFor = was })
}

// crash is the panic a crash at a step raises.
type crash struct{}

// mustRun is Run, failing the test on a refusal.
func mustRun(t *testing.T, p *Plan, s Start) *Result {
	t.Helper()
	r, err := Run(context.Background(), p, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return r
}
