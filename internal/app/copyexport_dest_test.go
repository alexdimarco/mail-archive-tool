package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// lsTree lists every entry under root, directories with a trailing slash, sorted: a
// refusal "writes nothing" when this is the same before and after (treeFiles sees only
// files, and Create folder's one side effect is an empty directory).
func lsTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			rel += "/"
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// destFixture is an archive (two stores, a few folders, a manifest) and, beside it, an
// empty destination, both under one temp directory.
func destFixture(t *testing.T) (top, arch, dest string) {
	t.Helper()
	top = t.TempDir()
	arch = filepath.Join(top, "archive")
	for _, d := range []string{"store1/Inbox/2026-03", "store1/Sent", "store2/Inbox"} {
		if err := os.MkdirAll(filepath.Join(arch, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(arch, ".mailarchive-manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest = filepath.Join(top, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	return top, arch, dest
}

// idOfDir reads the identity of the directory at path.
func idOfDir(t *testing.T, path string) ceIdentity {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	id, err := readIdentity(f)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// refusedWith asserts err is the typed refusal id, naming each needle, with nothing
// written under the given trees.
func refusedWith(t *testing.T, err error, id string, names []string, trees ...string) {
	t.Helper()
	var ref *CopyRefusal
	if !errors.As(err, &ref) {
		t.Fatalf("want refusal %s, got %v", id, err)
	}
	if ref.ID != id {
		t.Fatalf("refusal = %s (%q), want %s", ref.ID, ref.Message, id)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(names...))
}

// covers: MA-414, R29, R4, R12, S52
// Destination refusals by path: a missing folder, a file, a relative path and a NUL are
// R-e; the configured archive itself, a folder inside it and a folder reached through a
// symlinked parent are R-f, each with nothing written to the destination or the archive;
// the positive twin (a folder beside the archive, and one that contains it) is accepted;
// a cloud-synced destination is a warning naming the service, not a refusal.
func TestDestinationRefusals(t *testing.T) {
	top, arch, dest := destFixture(t)
	e := newCEEnv(t)
	archives := []string{arch}
	snap := func() ([]string, []string) { return lsTree(t, dest), lsTree(t, arch) }
	d0, a0 := snap()

	// the positive twins first
	if _, err := e.CheckDestination(dest, archives); err != nil {
		t.Fatalf("a folder beside the archive was refused: %v", err)
	}
	if _, err := e.CheckDestination(top, archives); err != nil {
		t.Fatalf("a folder that contains the archive is not inside it, but was refused: %v", err)
	}

	file := filepath.Join(dest, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d0, a0 = snap()
	link := filepath.Join(top, "alias")
	linked := os.Symlink(arch, link) == nil

	for _, c := range []struct {
		name, dest, id string
		names          []string
		skip           bool
	}{
		{"missing", filepath.Join(top, "nope"), "R-e", []string{"doesn't exist", "Browse"}, false},
		{"a file", file, "R-e", []string{"doesn't exist", "Create folder"}, false},
		{"relative", "dest", "R-e", []string{"full path", "Browse"}, false},
		{"a NUL", dest + "\x00x", "R-e", []string{"full path"}, false},
		{"the archive", arch, "R-f", []string{"inside your archive", arch}, false},
		{"inside the archive", filepath.Join(arch, "store1", "Inbox"), "R-f", []string{"inside your archive", "Choose a folder outside"}, false},
		{"not yet made, inside the archive", filepath.Join(arch, "store1", "New"), "R-e", []string{"doesn't exist"}, false},
		{"through a symlinked parent", filepath.Join(link, "store1", "Sent"), "R-f", []string{"inside your archive"}, !linked},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.skip {
				t.Skip("this host cannot make a symlink")
			}
			_, err := e.CheckDestination(c.dest, archives)
			refusedWith(t, err, c.id, c.names)
			if d1, a1 := snap(); !reflect.DeepEqual(d1, d0) || !reflect.DeepEqual(a1, a0) {
				t.Errorf("the refusal wrote something: dest %v -> %v, archive %v -> %v", d0, d1, a0, a1)
			}
		})
	}

	// through the plan the same refusal is typed and nothing is made
	_, err := e.PlanCopyExport(CopyPlanInput{Source: CopySourceInfo{Kind: "m365"}, Messages: ceMsgs(1), Dest: filepath.Join(arch, "store2"),
		Options: CopyOptions{PreserveFolders: true}, Archives: archives})
	refusedWith(t, err, "R-f", []string{"inside your archive"})

	// a cloud-synced destination is a warning
	cloud := filepath.Join(top, "OneDrive", "exports")
	if err := os.MkdirAll(cloud, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := e.PlanCopyExport(CopyPlanInput{Source: CopySourceInfo{Kind: "m365"}, Messages: ceMsgs(1), Dest: cloud,
		Options: CopyOptions{PreserveFolders: true}, Archives: archives})
	if err != nil {
		t.Fatalf("a cloud-synced destination was refused: %v", err)
	}
	if !strings.Contains(strings.Join(p.Warnings, " | "), "OneDrive") {
		t.Errorf("no warning names the cloud service: %q", p.Warnings)
	}
}

// covers: MA-414, R29, R4, R12, S52
// By identity too (RR2-C13): a destination that is an alias of the archive's root, of a
// folder inside it (a bind mount of <archive>/<store>), or whose ancestor is one, is R-f
// with nothing written, from the identities of every directory of the archive against
// the destination's and each ancestor's; a folder with another identity is accepted.
// The alias is the identity seam here (a bind mount needs privileges); the real alias
// is the Linux and Windows legs' row text.
func TestDestinationAliasesAreRefusedByIdentity(t *testing.T) {
	_, arch, dest := destFixture(t)
	e := newCEEnv(t)
	own := idOfDir(t, dest)
	d0, a0 := lsTree(t, dest), lsTree(t, arch)

	e.destIDs = func(string) ([]ceIdentity, error) { return []ceIdentity{own}, nil }
	if _, err := e.CheckDestination(dest, []string{arch}); err != nil {
		t.Fatalf("the positive twin, a destination with its own identity, was refused: %v", err)
	}

	for name, alias := range map[string][]ceIdentity{
		"the archive's root":               {idOfDir(t, arch)},
		"a folder inside it (the store)":   {idOfDir(t, filepath.Join(arch, "store1"))},
		"a deep folder inside it":          {idOfDir(t, filepath.Join(arch, "store1", "Inbox", "2026-03"))},
		"an ancestor of the destination":   {own, idOfDir(t, filepath.Join(arch, "store2"))},
		"the store, below a long ancestry": {own, own, idOfDir(t, filepath.Join(arch, "store1", "Sent")), own},
	} {
		t.Run(name, func(t *testing.T) {
			e.destIDs = func(string) ([]ceIdentity, error) { return alias, nil }
			_, err := e.CheckDestination(dest, []string{arch})
			refusedWith(t, err, "R-f", []string{"inside your archive", arch})
			if d1, a1 := lsTree(t, dest), lsTree(t, arch); !reflect.DeepEqual(d1, d0) || !reflect.DeepEqual(a1, a0) {
				t.Errorf("the refusal wrote something")
			}
		})
	}

	// Start repeats it against the set the preview collected (the walk is not repeated)
	e.destIDs = func(string) ([]ceIdentity, error) { return []ceIdentity{own}, nil }
	out, err := e.CheckDestination(dest, []string{arch})
	if err != nil {
		t.Fatal(err)
	}
	e.destIDs = func(string) ([]ceIdentity, error) {
		return []ceIdentity{idOfDir(t, filepath.Join(arch, "store1"))}, nil
	}
	refusedWith(t, out.Recheck(e, dest), "R-f", []string{"inside your archive"})
}

// covers: MA-414, R29, R4, R12, S52
// Where identity cannot decide the answer is R-f2, nothing written: through the view
// seam, a share served by this computer, a destination on another share than a
// share-held archive's, and an sshfs or overlay mount; the positive twins (a share on
// another computer beside a local archive, the reverse, and two folders on one share)
// are allowed; an archive walk past its bound, or one that meets an unreadable
// directory, with the destination on the archive's volume, is R-f2, and is not when
// the destination is on another volume; an archive on a view that cannot be compared
// is R-f2 for every destination, in the archive-side words.
func TestDestinationViewsThatCannotBeCompared(t *testing.T) {
	_, arch, dest := destFixture(t)
	archReal := util.ResolveExisting(arch)
	local := ceView{Kind: viewLocal}
	share := func(server, mount string) ceView { return ceView{Kind: viewShare, Server: server, Mount: mount} }
	here := func(s string) bool { return s == "thiscomputer" || s == "localhost" }

	run := func(av, dv ceView) error {
		e := newCEEnv(t)
		e.served = here
		e.view = func(p string) ceView {
			if pathUnder(filepath.ToSlash(util.ResolveExisting(p)), filepath.ToSlash(archReal)) {
				return av
			}
			return dv
		}
		_, err := e.CheckDestination(dest, []string{arch})
		return err
	}
	for _, c := range []struct {
		name   string
		av, dv ceView
		id     string // "" = allowed
		names  []string
	}{
		{"two local folders, identity decides: allowed", local, local, "", nil},
		{"a share on another computer beside a local archive", local, share("nas", "//nas/mail"), "", nil},
		{"a local destination beside a share-held archive", share("nas", "//nas/mail"), local, "", nil},
		{"two folders of one share", share("nas", "//nas/mail"), share("nas", "//nas/mail"), "", nil},
		{"a share served by this computer", local, share("thiscomputer", "//thiscomputer/x"), "R-f2", []string{"share on this computer", "local path", dest}},
		{"another share than a share-held archive's", share("nas", "//nas/mail"), share("nas", "//nas/other"), "R-f2", []string{"different network shares", "archive's own share"}},
		{"an sshfs or overlay destination", local, ceView{Kind: viewStacking, Type: "fuse.sshfs"}, "R-f2", []string{"sshfs", "SMB or NFS client"}},
		{"an archive on a share this computer serves", share("localhost", "//localhost/mail"), local, "R-f2", []string{"Your archive at", "share on this computer", "local path"}},
		{"an archive on an sshfs mount", ceView{Kind: viewStacking, Type: "fuse.sshfs"}, local, "R-f2", []string{"Your archive is on an sshfs", "SMB or NFS client"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := run(c.av, c.dv)
			if c.id == "" {
				if err != nil {
					t.Fatalf("the positive twin was refused: %v", err)
				}
				return
			}
			refusedWith(t, err, c.id, c.names)
		})
	}

	t.Run("a walk past its bound", func(t *testing.T) {
		e := newCEEnv(t)
		e.walkDirs = 3 // the fixture has more directories than that
		_, err := e.CheckDestination(dest, []string{arch})
		refusedWith(t, err, "R-f2", []string{"too large or partly unreadable", "another drive"})

		// the destination on another volume: the set did not have to decide
		e.destIDs = func(string) ([]ceIdentity, error) { return []ceIdentity{{Dev: 1 << 40, Ino: 7}}, nil }
		if _, err := e.CheckDestination(dest, []string{arch}); err != nil {
			t.Errorf("a destination on another volume was refused over a partial walk: %v", err)
		}
	})

	t.Run("an unreadable directory", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("a mode-000 directory is still listable here (Windows, or root); the Windows leg uses a denying ACE")
		}
		locked := filepath.Join(arch, "store1", "locked")
		if err := os.Mkdir(locked, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
		e := newCEEnv(t)
		_, err := e.CheckDestination(dest, []string{arch})
		refusedWith(t, err, "R-f2", []string{"too large or partly unreadable"})
	})
}

// covers: MA-414, R29, R4, R12, S52
// The classifier reads each view from inputs that see a share and name its server, not
// from the filesystem type alone: recorded /proc/self/mountinfo lines classify cifs,
// nfs4, fuse.sshfs, overlay, a FUSE block device and a plain local mount (the longest
// mount point holds the path, escapes are read); statfs's names classify smbfs, nfs and
// macFUSE; a gvfs path names its share and never decides by identity; a share whose
// server is this computer is R-f2 whatever its filesystem type, which is what the
// type-alone classifier (the \\localhost case) would miss; a local temp folder is local.
func TestViewClassifier(t *testing.T) {
	lines := []string{
		`24 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw`,
		`40 24 0:35 / /mnt/nas rw,relatime - cifs //nas.example.com/mail rw,vers=3.0`,
		`41 24 0:36 / /mnt/nfs rw,relatime - nfs4 filer.example.com:/export/mail rw`,
		`42 24 0:37 / /mnt/ssh rw,nosuid - fuse.sshfs alex@host:/home/alex rw`,
		`43 24 0:38 / /mnt/over rw - overlay overlay rw,lowerdir=/a,upperdir=/b`,
		`44 24 8:17 / /mnt/ntfs rw - fuseblk /dev/sdb1 rw`,
		`45 24 0:39 / /mnt/with\040space rw - cifs //LOCALHOST/Share rw`,
		`46 40 0:40 / /mnt/nas/deeper rw - ext4 /dev/sdc1 rw`,
	}
	for _, c := range []struct {
		path   string
		kind   ceViewKind
		server string
	}{
		{"/home/alex/mail", viewLocal, ""},
		{"/mnt/nas/export1", viewShare, "nas.example.com"},
		{"/mnt/nas/deeper/x", viewLocal, ""}, // the longest mount point holds it
		{"/mnt/nfs/a/b", viewShare, "filer.example.com"},
		{"/mnt/ssh/x", viewStacking, ""},
		{"/mnt/over/x", viewStacking, ""},
		{"/mnt/ntfs/x", viewLocal, ""}, // a FUSE block filesystem on a device is a local one
		{"/mnt/with space/x", viewShare, "localhost"},
		{"/mnt/nasty/x", viewLocal, ""}, // /mnt/nas is not a prefix of /mnt/nasty
	} {
		got := classifyMountinfo(lines, c.path)
		if got.Kind != c.kind || got.Server != c.server {
			t.Errorf("%s classifies as %+v, want kind %d server %q", c.path, got, c.kind, c.server)
		}
	}
	if v := classifyMountinfo(lines, "/run/user/1000/gvfs/smb-share:server=NAS,share=mail/x"); v.Kind != viewShare || v.Server != "nas" || !v.NoID {
		t.Errorf("a gvfs path = %+v, want a share on nas whose ids are its own", v)
	}
	if v := classifyMountinfo(nil, "/x"); v.Kind != viewStacking {
		t.Errorf("a mountinfo that cannot be read is %+v, want stacking", v)
	}
	for _, c := range []struct {
		fstype, from string
		kind         ceViewKind
		server       string
	}{
		{"smbfs", "//alex@NAS/share", viewShare, "nas"},
		{"nfs", "filer:/vol/mail", viewShare, "filer"},
		{"macfuse", "sshfs@host", viewStacking, ""},
		{"osxfuse", "x", viewStacking, ""},
		{"apfs", "/dev/disk3s1", viewLocal, ""},
		{"msdos", "/dev/disk4s1", viewLocal, ""},
		{"somethingnew", "x", viewStacking, ""}, // a type it does not know is stacking
	} {
		if got := classifyFSName(c.fstype, c.from); got.Kind != c.kind || got.Server != c.server {
			t.Errorf("statfs %q %q = %+v, want kind %d server %q", c.fstype, c.from, got, c.kind, c.server)
		}
	}

	// A share on this computer is R-f2 whatever its type says: classified from the
	// mount source, \\localhost\mail (cifs, NTFS-looking) is served here.
	_, arch, dest := destFixture(t)
	e := newCEEnv(t)
	archReal := util.ResolveExisting(arch)
	e.view = func(p string) ceView {
		if pathUnder(filepath.ToSlash(util.ResolveExisting(p)), filepath.ToSlash(archReal)) {
			return ceView{Kind: viewLocal}
		}
		return classifyMountinfo([]string{`50 1 0:50 / ` + filepath.ToSlash(dest) + ` rw - cifs //localhost/mail rw`}, filepath.ToSlash(dest))
	}
	_, err := e.CheckDestination(dest, []string{arch})
	refusedWith(t, err, "R-f2", []string{"share on this computer"})

	for _, s := range []string{"localhost", "LOCALHOST", "127.0.0.1", "[::1]"} {
		if !(&CopyExportEnv{}).servedHere(s) {
			t.Errorf("%q is not read as this computer", s)
		}
	}
	if host, _ := os.Hostname(); host != "" && !(&CopyExportEnv{}).servedHere(host) {
		t.Errorf("this computer's own name %q is not read as this computer", host)
	}
	if (&CopyExportEnv{}).servedHere("some-other-computer.example") {
		t.Errorf("another computer's name is read as this one")
	}

	// the real classifier on this host's temp folder: a local disk
	if v := classifyView(t.TempDir()); v.Kind != viewLocal {
		t.Errorf("this host's temp folder classifies as %+v, want local", v)
	}
}

// covers: MA-434, R4, R29, R12
// Create folder makes exactly one directory, at 0700, with a sanitized single-segment
// name under an existing folder outside the archive, through the handle its checks
// opened. Refused with nothing created: an existing name (R-q, naming the folder), a
// separator, "..", ".", a reserved name and an empty one (R-q with the sanitized
// suggestion), a parent inside the archive or reached through an alias of a folder
// inside it (R-f), and a view that cannot be confirmed (R-f2). A parent swapped for a
// symlink into the archive between the check and the mkdir (a fault hook) puts no
// directory inside the archive.
func TestCreateFolderMakesExactlyOneChecked(t *testing.T) {
	top, arch, dest := destFixture(t)
	e := newCEEnv(t)
	archives := []string{arch}

	before := lsTree(t, dest)
	made, err := e.CreateFolder(dest, "Archive Folder", archives)
	if err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	if made != filepath.Join(dest, "Archive Folder") {
		t.Errorf("created %q, want the path under the destination", made)
	}
	if got := lsTree(t, dest); !reflect.DeepEqual(got, append(before, "Archive Folder/")) {
		t.Errorf("exactly one directory should have appeared: %v -> %v", before, got)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(made); fi == nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("the new folder's mode = %v, want 0700", fi)
		}
	}

	d0, a0 := lsTree(t, dest), lsTree(t, arch)
	nothing := func(t *testing.T) {
		t.Helper()
		if d1, a1 := lsTree(t, dest), lsTree(t, arch); !reflect.DeepEqual(d1, d0) || !reflect.DeepEqual(a1, a0) {
			t.Errorf("the refusal created something: dest %v -> %v, archive %v -> %v", d0, d1, a0, a1)
		}
	}
	for _, c := range []struct {
		name, parent, folder string
		id                   string
		names                []string
	}{
		{"an existing name", dest, "Archive Folder", "R-q", []string{"'Archive Folder' already exists", "Browse"}},
		{"a separator", dest, "a/b", "R-q", []string{"'a/b' can't be a folder name", "contains '/'", "try 'a b'"}},
		{"a backslash", dest, `a\b`, "R-q", []string{"contains '\\'"}},
		{"dot-dot", dest, "..", "R-q", []string{"can't be a folder name"}},
		{"dot", dest, ".", "R-q", []string{"can't be a folder name"}},
		{"a reserved name", dest, "CON", "R-q", []string{"can't be a folder name", "CON"}},
		{"empty", dest, "", "R-q", []string{"can't be a folder name", "empty"}},
		{"a parent inside the archive", filepath.Join(arch, "store1"), "New", "R-f", []string{"inside your archive"}},
		{"the archive itself", arch, "New", "R-f", []string{"inside your archive"}},
		{"a missing parent", filepath.Join(top, "nope"), "New", "R-e", []string{"doesn't exist"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := e.CreateFolder(c.parent, c.folder, archives)
			refusedWith(t, err, c.id, c.names)
			nothing(t)
		})
	}

	t.Run("a parent reached through an alias of a folder inside the archive", func(t *testing.T) {
		alias := idOfDir(t, filepath.Join(arch, "store1"))
		e2 := newCEEnv(t)
		e2.destIDs = func(string) ([]ceIdentity, error) { return []ceIdentity{alias}, nil }
		_, err := e2.CreateFolder(dest, "New", archives)
		refusedWith(t, err, "R-f", []string{"inside your archive"})
		nothing(t)
	})
	t.Run("a view that cannot be confirmed", func(t *testing.T) {
		e2 := newCEEnv(t)
		archReal := util.ResolveExisting(arch)
		e2.view = func(p string) ceView {
			if pathUnder(filepath.ToSlash(util.ResolveExisting(p)), filepath.ToSlash(archReal)) {
				return ceView{Kind: viewLocal}
			}
			return ceView{Kind: viewStacking, Type: "fuse.sshfs"}
		}
		_, err := e2.CreateFolder(dest, "New", archives)
		refusedWith(t, err, "R-f2", []string{"couldn't confirm"})
		nothing(t)
	})

	t.Run("a parent swapped for a symlink into the archive after the check", func(t *testing.T) {
		parent := filepath.Join(top, "swappable")
		if err := os.Mkdir(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		store := filepath.Join(arch, "store2")
		e2 := newCEEnv(t)
		swapped := false
		e2.faults = &copyFaults{hook: func(step string) {
			if step != "W1check" {
				return
			}
			if err := os.Rename(parent, parent+".moved"); err != nil {
				t.Skipf("this host cannot rename the parent: %v", err)
			}
			if err := os.Symlink(store, parent); err != nil {
				t.Skipf("this host cannot make a symlink: %v", err)
			}
			swapped = true
		}}
		a1 := lsTree(t, arch)
		if _, err := e2.CreateFolder(parent, "Escaped", archives); err != nil {
			t.Fatalf("the folder was refused: %v", err)
		}
		if !swapped {
			t.Fatal("the fault hook did not run between the check and the mkdir")
		}
		if got := lsTree(t, arch); !reflect.DeepEqual(got, a1) {
			t.Errorf("a directory appeared inside the archive: %v -> %v", a1, got)
		}
		if _, err := os.Stat(filepath.Join(parent+".moved", "Escaped")); err != nil {
			t.Errorf("the folder is not in the parent the checks opened: %v", err)
		}
	})
}
