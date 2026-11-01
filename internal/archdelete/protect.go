package archdelete

import (
	"os"
	"path/filepath"
	"runtime"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
)

// Protected are the folders §7's protected row names [C15]. Each is compared by
// file identity (os.SameFile up the ancestor chains of the path as given and as
// resolved), never by path text, so another spelling — a different case, an 8.3
// name, a subst drive, a link above it — is the same folder.
type Protected struct {
	// Config, Scratch and Record are MailArchive's settings folder, its scratch
	// folder and this computer's record of encrypted archives: Delete refuses a
	// folder that is, contains or is inside one.
	Config, Scratch, Record string
	// Settings are further settings folders refused as Config is: the Desktop's own when it
	// was started with -config (filepath.Dir of that file), which is not job.Dir() (MA-973).
	Settings []string
	// Archives are the known archives (the rows of Your archives): Delete refuses a
	// folder that contains or is inside another one; the row that is this folder is
	// not another.
	Archives []string
	// Home is the home folder; System the operating system's folders: Delete
	// refuses a folder that is or contains one. A drive root is refused too.
	Home   string
	System []string
}

// DefaultProtected is this computer's protected folders, the known archives given
// by the caller.
func DefaultProtected(archives []string) Protected {
	p := Protected{Archives: archives, System: systemFolders()}
	p.Config, _ = job.Dir()
	p.Scratch, _ = job.ScratchDir()
	p.Record, _ = encrecord.Dir()
	p.Home, _ = os.UserHomeDir()
	return p
}

// RecordLookup is this computer's record of the encrypted archive at folder, found
// while the folder exists (§3.1 [NF-2]): a record whose recorded folder is this
// folder — not one above it — gives its ID and recorded Spelling.
func RecordLookup(folder string) (RecordRef, error) {
	rec, err := encrecord.Load()
	if err != nil {
		return RecordRef{}, err
	}
	m, ok := rec.Lookup(folder)
	if !ok || !sameFolder(m.Folder, folder, runtime.GOOS) {
		return RecordRef{}, nil
	}
	return RecordRef{Recorded: true, ID: m.ID, Spelling: m.Path.Spelling}, nil
}

// checkProtected refuses a protected folder (§7): "MailArchive will not delete this
// folder: it is (or is inside, or holds) NAME. Delete the archive's files by hand."
func checkProtected(abs string, p Protected) error {
	for _, d := range append([]string{p.Config, p.Scratch, p.Record}, p.Settings...) {
		if d == "" {
			continue
		}
		if is, inside, contains := relation(abs, d); is || inside || contains {
			return protectedRefusal(d)
		}
	}
	for _, a := range p.Archives {
		if a == "" {
			continue
		}
		if is, inside, contains := relation(abs, a); !is && (inside || contains) {
			return protectedRefusal(a)
		}
	}
	for _, d := range append([]string{p.Home}, p.System...) {
		if d == "" {
			continue
		}
		if is, _, contains := relation(abs, d); is || contains {
			return protectedRefusal(d)
		}
	}
	if driveRoot(abs) {
		return protectedRefusal(abs)
	}
	return nil
}

func protectedRefusal(name string) error {
	return refuse(CaseProtected, "MailArchive will not delete this folder: it is (or is inside, or holds) %s. Delete the archive's files by hand.", name)
}

// relation says whether folder a is b, is inside b, or contains b, by file
// identity: b's identity is looked for on a's ancestor chain and a's on b's, each
// chain taken both as spelled and with its links resolved. A b that cannot be read
// is no relation.
func relation(a, b string) (is, inside, contains bool) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, false, false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, false, false
	}
	if os.SameFile(fa, fb) {
		return true, false, false
	}
	return false, onChain(a, fb), onChain(b, fa)
}

// onChain reports whether target is a strict ancestor of p, spelled or resolved.
func onChain(p string, target os.FileInfo) bool {
	abs := cleanAbs(p)
	for _, start := range []string{abs, util.ResolveExisting(abs)} {
		for d := filepath.Dir(start); ; d = filepath.Dir(d) {
			if fi, err := os.Stat(d); err == nil && os.SameFile(fi, target) {
				return true
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return false
}

// driveRoot reports a folder that is a drive root: the top of its path, or the
// top of a volume mounted inside another (a USB drive under /Volumes, a folder a
// volume is mounted on).
func driveRoot(abs string) bool {
	parent := filepath.Dir(abs)
	if parent == abs {
		return true
	}
	v, ok := volumeOf(abs)
	pv, pok := volumeOf(parent)
	return ok && pok && v != pv
}

// systemFolders are the operating system's folders §7 names.
func systemFolders() []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		for _, v := range []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramData"} {
			if d := os.Getenv(v); d != "" {
				out = append(out, d)
			}
		}
		return out
	case "darwin":
		return []string{"/System", "/Library", "/Applications", "/usr"}
	default:
		return []string{"/usr", "/etc", "/var", "/bin", "/opt"}
	}
}
