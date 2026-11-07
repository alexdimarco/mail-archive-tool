//go:build darwin

package tbrestore

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// mkdirPrivate and createPrivate (macOS; settled 2026-11-07 [R7-01]): an entry is
// readable by this account alone from the instant it exists. A folder made with mkdir
// takes Local Folders' inheritable access-list entries at birth, whatever its mode says,
// and a handle another account opens before FS.Private removes them keeps its right to
// list it. So an entry made straight in Local Folders (the container, the parent file)
// is made with mkdir_extended / open_extended and an empty access list flagged
// KAUTH_ACL_NO_INHERIT, which the kernel reads as "inherit nothing" (kauth_acl_inherit:
// "initial object creation via open_extended() and mkdir_extended() [may] reject
// inheritance"), at mode 0700 / 0600, which the umask can only narrow. Those calls take a
// path, not a folder handle, so the new entry is then bound to the root: the root's look
// at the name and the path's must be the same entry, of the kind made. Everything else is
// made inside the container, which has no inheritable entry, so mkdirat 0700 and openat
// 0600 through the root are owner-only at birth too. FS.Private then reads each back.
//
// Settled 2026-11-07 [R7-01 independent F1]: a volume that holds no access list (FAT32,
// exFAT) refuses the access-list calls ("invalid argument"), so the call is chosen by
// asking the volume (topCreate), not by trying: where it holds none there is nothing to
// inherit, and the plain exclusive create through the root (0700 / 0600) is owner-only at
// birth as far as that volume can say; the plan already treats such a drive as not
// private (W-READERS, asked twice for an encrypted archive).
func mkdirPrivate(root *os.Root, name string) error {
	if strings.Contains(name, "/") {
		return root.Mkdir(name, 0o700)
	}
	if ext, err := topCreate(root); err != nil {
		return err
	} else if !ext {
		return root.Mkdir(name, 0o700)
	}
	path := filepath.Join(root.Name(), name)
	sec := noInheritFilesec()
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return err
	}
	if _, _, e := unix.Syscall6(unix.SYS_MKDIR_EXTENDED, uintptr(unsafe.Pointer(p)), kauthNone, kauthNone, 0o700, uintptr(unsafe.Pointer(&sec[0])), 0); e != 0 {
		return refusedCall(root, &os.PathError{Op: "mkdir", Path: name, Err: e})
	}
	afterPathCreateSeam(name)
	fi, err := os.Lstat(path)
	if err != nil {
		return madeElsewhere(root, true, err)
	}
	return bindCreated(root, name, fi, true)
}

func createPrivate(root *os.Root, name string) (wfile, error) {
	if strings.Contains(name, "/") {
		return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	if ext, err := topCreate(root); err != nil {
		return nil, err
	} else if !ext {
		return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	path := filepath.Join(root.Name(), name)
	sec := noInheritFilesec()
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return nil, err
	}
	flags := unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, _, e := unix.Syscall6(unix.SYS_OPEN_EXTENDED, uintptr(unsafe.Pointer(p)), uintptr(flags), kauthNone, kauthNone, 0o600, uintptr(unsafe.Pointer(&sec[0])))
	if e != 0 {
		return nil, refusedCall(root, &os.PathError{Op: "open", Path: name, Err: e})
	}
	f := os.NewFile(fd, path)
	afterPathCreateSeam(name)
	fi, err := f.Stat()
	if err == nil {
		err = bindCreated(root, name, fi, false)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// afterPathCreateSeam runs between a create by path and the bind's look; a test swaps a
// folder on the way there. It does nothing in production.
var afterPathCreateSeam = func(name string) {}

// pcExtendedSecurityNP is _PC_EXTENDED_SECURITY_NP (sys/unistd.h): pathconf answers 1
// when the volume holds access lists.
const pcExtendedSecurityNP = 13

// extendedSecurityOf asks the volume the root is on, through the root's own open folder
// (no path look), whether it holds access lists. A test replaces it.
var extendedSecurityOf = func(root *os.Root) (bool, error) {
	f, err := root.Open(".")
	if err != nil {
		return false, err
	}
	defer f.Close()
	v, err := unix.Fpathconf(int(f.Fd()), pcExtendedSecurityNP)
	if err != nil {
		return false, err
	}
	return v == 1, nil
}

// topCreate says whether an entry straight in Local Folders is made with the access-list
// call (true) or the plain exclusive create (false). When the volume cannot be asked, the
// plain create is chosen only if the volume is also detected as not private (its format,
// or ownership ignored; the plan's own test); on any other volume nothing is made and the
// words say so (W-NOTPRIVATE), never a silent plain create.
func topCreate(root *os.Root) (bool, error) {
	ext, err := extendedSecurityOf(root)
	if err == nil {
		return ext, nil
	}
	if FormatCannotLimitReaders(fsType(root.Name())) || OwnersIgnored(root.Name()) {
		return false, nil
	}
	return false, notPrivateDrive(root, err)
}

// refusedCall is an access-list create the volume refused although it said it holds
// access lists (EINVAL, ENOTSUP): W-NOTPRIVATE in plain words, never "invalid argument".
// Any other error (the name taken, a full disk) is returned as it is.
func refusedCall(root *os.Root, err error) error {
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) {
		return notPrivateDrive(root, err)
	}
	return err
}

// wNotPrivateDrive (W-NOTPRIVATE; R7-01 independent F1, the lead's call: plain words that
// name the drive's format and what to do): nothing was made.
const wNotPrivateDrive = "MailArchive could not make the restored mail readable only by your account on the drive that holds Thunderbird's Local Folders (PATH, formatted FORMAT): the drive did not accept the setting that keeps other accounts out, so nothing was added to Thunderbird. Choose a profile whose Local Folders is on this computer's own drive."

func notPrivateDrive(root *os.Root, cause error) error {
	return refuse("W-NOTPRIVATE", fill(wNotPrivateDrive, "PATH", root.Name(), "FORMAT", formatName(fsType(root.Name()))), cause)
}

// formatName is a volume's file system as the Finder names it.
func formatName(fsType string) string {
	switch strings.ToLower(fsType) {
	case "apfs":
		return "APFS"
	case "hfs":
		return "Mac OS Extended"
	case "msdos":
		return "FAT (MS-DOS)"
	case "exfat":
		return "exFAT"
	case "smbfs":
		return "a network share (SMB)"
	case "afpfs":
		return "a network share (AFP)"
	case "webdav":
		return "a network share (WebDAV)"
	case "":
		return "a format macOS did not name"
	}
	return fsType
}

// kauthNone is KAUTH_UID_NONE and KAUTH_GID_NONE (~0 - 100): owner and group left as made.
const kauthNone = uintptr(0xFFFFFF9B)

// noInheritFilesec is a kauth_filesec (native byte order, as the *_extended calls read
// it): magic, no owner or group GUID, an access list of no entries whose flags are
// KAUTH_ACL_NO_INHERIT.
func noInheritFilesec() []byte {
	const (
		filesecMagic = 0x012CC16D
		aclNoInherit = 1 << 17
	)
	b := make([]byte, 4+16+16+4+4)
	binary.NativeEndian.PutUint32(b[0:4], filesecMagic)
	binary.NativeEndian.PutUint32(b[36:40], 0)
	binary.NativeEndian.PutUint32(b[40:44], aclNoInherit)
	return b
}

// bindCreated checks that what the path made is what the root sees at name: the same
// entry, of the kind made, not a link (a folder on the way swapped between the two would
// show a different entry, and the write stops before anything is put in it).
//
// Settled 2026-11-07 [R7-01 independent F3]: and this user's own. A folder on the way
// swapped away for the create and back before the look, with an entry planted at the name,
// passes the identity check (the path and the root then both see the planted entry); one
// planted by another account cannot be this user's. On a volume that ignores ownership
// every entry reads as the mount owner's, so there the owner says nothing (the plan asks
// twice there for an encrypted archive) and is not required.
func bindCreated(root *os.Root, name string, made os.FileInfo, dir bool) error {
	got, err := root.Lstat(name)
	if err != nil {
		return madeElsewhere(root, dir, err)
	}
	if !os.SameFile(made, got) || got.IsDir() != dir || got.Mode()&os.ModeSymlink != 0 || made.Mode()&os.ModeSymlink != 0 {
		return madeElsewhere(root, dir, errChanged)
	}
	if st, ok := got.Sys().(*syscall.Stat_t); !ok || (int(st.Uid) != os.Getuid() && !OwnersIgnored(root.Name())) {
		return madeElsewhere(root, dir, errChanged)
	}
	return nil
}

// madeElsewhere is a create by path the root could not bind (W-CHANGED; R7-01 independent
// F3 and F6): a folder on the way changed while the restore wrote. What the call made
// cannot be shown to be the entry the path now names (it took no handle on the folder it
// was made in), so nothing is removed: neither it, wherever it went, nor what stands under
// the name in Local Folders, which is not this restore's. The words say so plainly, never
// the raw error.
func madeElsewhere(root *os.Root, dir bool, cause error) error {
	kind := "file"
	if dir {
		kind = "folder"
	}
	return refuse("W-CHANGED", fill(wChanged, "WHAT", fill(wMadeElsewhere, "PATH", root.Name(), "KIND", kind)), cause)
}

// wMadeElsewhere is W-CHANGED's WHAT for madeElsewhere.
const wMadeElsewhere = "a folder on the way to Thunderbird's Local Folders (PATH) changed while the restore was writing, so it stopped and nothing was added to Thunderbird. The empty KIND MailArchive had just made may have gone to another place; MailArchive left it there and removed nothing, since it cannot be sure it is its own"
