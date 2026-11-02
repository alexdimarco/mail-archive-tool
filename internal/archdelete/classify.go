package archdelete

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

const manifestName = ".mailarchive-manifest.json"

// manifestCap bounds §4.1's manifest read: sized for real archives, as the copy
// export's read of another archive is (app/copyexport_archive.go), never
// archindex.inspect's 1 MiB, which an archive of about 1,450 messages passes [C10].
// A var only so a test can stand in for a manifest over it without writing 1 GiB.
var manifestCap int64 = 1 << 30

// manifestSource is what readManifest reads the opened manifest through: the file itself. A
// var only so a test can inject a read fault (an I/O error part way through a real file)
// that no file on a test machine gives on demand (MA-1006).
var manifestSource = func(f *os.File) io.Reader { return f }

// linkLike is util.IsLinkLike as the plan and the row ask it of the folder and its manifest. A
// var only so a test can inject a check that fails, as reading a Windows reparse tag can, which
// no file on a test machine gives on demand (MA-980).
var linkLike = util.IsLinkLike

// manifestStat is readManifest's Stat of the opened manifest: the file's own. A var only so a
// test can inject a Stat that fails on a file already open (MA-1006).
var manifestStat = func(f *os.File) (os.FileInfo, error) { return f.Stat() }

// classify is §4's archive test: a plain archive (a manifest §4.1's reader parses),
// an encrypted one, or neither. A folder that is both — a plaintext manifest beside
// an encrypted archive's files, as encmark's case Other reads it — is mixed and
// refused.
//
// Whether it is encrypted is asked of encmark.Here — the folder's own markers
// (vault.EncryptedMarker's) or this computer's record of exactly this folder — never
// of the vault's markers directly: MA-770's gate (encmark's
// TestEveryCallerAsksEncmark) holds every caller outside encmark and vault to that
// (the design's §4 names vault.EncryptedMarker; for the operator). A recorded
// folder stripped of its key files and replanted with a plaintext manifest is then
// mixed, never planned as a plain archive.
//
// Only a manifest that is not there, is not a regular file, or does not parse
// makes a folder "no longer an archive" (§4's test). A manifest that is there but
// cannot be read — no permission, an I/O error, a Windows sharing violation while a
// backup or an antivirus scan holds it, over the cap — or that a newer MailArchive
// wrote is refused with its own cause, never as "not an archive" and never as
// "reconnect" (del-s1a-A1-03, A2-03, A3-05, conformance D2; §7 has no row for these,
// for the operator).
func classify(root *os.Root, abs string) (Kind, error) {
	return classifyWith(root, abs, func(root *os.Root, _ string, _ os.FileInfo) error {
		_, _, err := readManifest(root)
		return err
	})
}

// classifyWith is classify with the manifest's parse asked of parse (fi: the manifest's Lstat),
// which a row keeps between page loads (rowArchiveTest).
func classifyWith(root *os.Root, abs string, parse func(root *os.Root, abs string, fi os.FileInfo) error) (Kind, error) {
	enc, _, merr := encmark.Here(abs)
	if enc {
		if vault.ManifestIsPlaintext(filepath.Join(abs, manifestName)) {
			return 0, refuse(CaseMixed, msgMixed)
		}
		return Encrypted, nil
	}
	if merr != nil {
		// The folder itself was opened (the caller holds an os.Root on it): a name in
		// it that cannot be read is that name's cause, not an unplugged drive.
		return 0, unreadableRefusal(pathOf(merr, abs), merr)
	}
	// A manifest that is itself a link is a name Delete would remove through a link.
	fi, err := root.Lstat(manifestName)
	switch {
	case isNotExist(err):
		return 0, refuse(CaseNotArchive, msgNotArchive)
	case err != nil:
		return 0, unreadableRefusal(filepath.Join(abs, manifestName), err)
	}
	if link, lerr := linkLike(filepath.Join(abs, manifestName)); fi.Mode()&fs.ModeSymlink != 0 || lerr != nil || link {
		return 0, linkRefusal(filepath.Join(abs, manifestName))
	}
	if err := parse(root, abs, fi); err != nil {
		return 0, manifestRefusal(abs, err)
	}
	return Plain, nil
}

// The manifest reader's failures, by what they mean for §4's archive test.
var errManifestTooLarge = errors.New("manifest over its cap")

// manifestReadError: the bytes could not be read. opened says the manifest was already open
// when the read failed (its Stat or a read part way through): whatever the error says, even one
// that counts as "not found" — ENOENT, or on Windows ERROR_PATH_NOT_FOUND or ERROR_BAD_NETPATH
// when a network folder drops — the file was there, so it is "cannot read", never "no longer an
// archive" (del-s5-A2-04).
type manifestReadError struct {
	err    error
	opened bool
}
type manifestParseError struct{ err error } // they were read and do not parse

func (e *manifestReadError) Error() string  { return e.err.Error() }
func (e *manifestReadError) Unwrap() error  { return e.err }
func (e *manifestParseError) Error() string { return e.err.Error() }
func (e *manifestParseError) Unwrap() error { return e.err }

// manifestRefusal is the refusal for a manifest readManifest could not give back.
func manifestRefusal(abs string, err error) error {
	path := filepath.Join(abs, manifestName)
	var newer *state.NewerFormatError
	var notRegular *util.NotRegularError
	var parse *manifestParseError
	var read *manifestReadError
	switch {
	case errors.As(err, &read) && read.opened:
		// Checked before isNotExist: a read of an opened manifest that failed is never
		// "no longer an archive" (del-s5-A2-04).
		return unreadableRefusal(path, err)
	case errors.As(err, &newer):
		return refuse(CaseNewerFormat, "This archive was written by a newer version of MailArchive (manifest format %d; this version understands %d). Update MailArchive, then delete it.", newer.Version, newer.Understood)
	case errors.Is(err, errManifestTooLarge):
		return refuse(CaseUnreadable, "This archive's manifest %s is larger than MailArchive can read (1 GiB). Delete the archive's files by hand.", path)
	case isNotExist(err), errors.As(err, &notRegular), errors.As(err, &parse):
		return refuse(CaseNotArchive, msgNotArchive)
	}
	return unreadableRefusal(path, err)
}

// unreadableRefusal is the refusal for a name in a reachable archive folder that
// cannot be read: it names the file and the cause, and the remedy is the person's.
func unreadableRefusal(path string, err error) error {
	cause := err.Error()
	var pe *fs.PathError
	if errors.As(err, &pe) {
		cause = pe.Err.Error()
	}
	return refuse(CaseUnreadable, "MailArchive cannot read %s (%s). Close any program using it, such as a backup or an antivirus scan, and make sure your account can open it, then click Delete again.", path, util.StripControl(cause))
}

// pathOf is the path an error names, or def.
func pathOf(err error, def string) string {
	var pe *fs.PathError
	if errors.As(err, &pe) && filepath.IsAbs(pe.Path) {
		return pe.Path
	}
	return def
}

// readManifest reads the manifest through root (util.OpenRegularFileIn: never
// through a link, never a pipe, a Windows placeholder accepted) under manifestCap,
// hashing the bytes it read, and parses them with state.LoadFrom — the snapshot
// path that resolves no store id on disk. A failure to read is a
// *manifestReadError; bytes that do not parse a *manifestParseError, except a
// newer format's *state.NewerFormatError, returned as it is.
func readManifest(root *os.Root) (*state.Manifest, FileFacts, error) {
	f, err := util.OpenRegularFileIn(root, manifestName)
	if err != nil {
		return nil, FileFacts{}, &manifestReadError{err: err}
	}
	defer f.Close()
	fi, err := manifestStat(f)
	if err != nil {
		return nil, FileFacts{}, &manifestReadError{err: err, opened: true}
	}
	h := sha256.New()
	data, err := io.ReadAll(io.TeeReader(io.LimitReader(manifestSource(f), manifestCap+1), h))
	if err != nil {
		return nil, FileFacts{}, &manifestReadError{err: err, opened: true}
	}
	if int64(len(data)) > manifestCap {
		return nil, FileFacts{}, errManifestTooLarge
	}
	m, err := state.LoadFrom(manifestName, data)
	if err != nil {
		var newer *state.NewerFormatError
		if errors.As(err, &newer) {
			return nil, FileFacts{}, err
		}
		return nil, FileFacts{}, &manifestParseError{err}
	}
	facts := FileFacts{Name: manifestName, Size: int64(len(data)), ModTime: fi.ModTime().UTC(), SHA256: hex.EncodeToString(h.Sum(nil))}
	return m, facts, nil
}

// hashFileIn is a regular file's facts read through root.
func hashFileIn(root *os.Root, name string) (FileFacts, error) {
	f, err := util.OpenRegularFileIn(root, name)
	if err != nil {
		return FileFacts{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return FileFacts{}, err
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return FileFacts{}, err
	}
	return FileFacts{Name: name, Size: n, ModTime: fi.ModTime().UTC(), SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// readSmallIn reads a regular file of at most max bytes through root.
func readSmallIn(root *os.Root, name string, max int64) ([]byte, error) {
	f, err := util.OpenRegularFileIn(root, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s is over %d bytes", name, max)
	}
	return data, nil
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
