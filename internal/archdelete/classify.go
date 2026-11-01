package archdelete

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

const manifestName = ".mailarchive-manifest.json"

// manifestCap bounds §4.1's manifest read: sized for real archives, as the copy
// export's read of another archive is (app/copyexport_archive.go), never
// archindex.inspect's 1 MiB, which an archive of about 1,450 messages passes [C10].
const manifestCap = 1 << 30

// classify is §4's archive test: a plain archive (a manifest §4.1's reader parses),
// an encrypted one (vault.EncryptedMarker), or neither. A folder that is both — a
// plaintext manifest beside an encrypted archive's files, as encmark's case Other
// reads it — is mixed and refused.
func classify(root *os.Root, abs string) (Kind, error) {
	enc, _, merr := vault.EncryptedMarker(abs)
	if enc {
		if vault.ManifestIsPlaintext(filepath.Join(abs, manifestName)) {
			return 0, refuse(CaseMixed, msgMixed)
		}
		return Encrypted, nil
	}
	if merr != nil {
		return 0, refuse(CaseUnreachable, msgUnreachable)
	}
	// A manifest that is itself a link is a name Delete would remove through a link.
	if fi, err := root.Lstat(manifestName); err == nil {
		if link, lerr := util.IsLinkLike(filepath.Join(abs, manifestName)); fi.Mode()&fs.ModeSymlink != 0 || lerr != nil || link {
			return 0, linkRefusal(filepath.Join(abs, manifestName))
		}
	}
	if _, _, err := readManifest(root); err != nil {
		return 0, refuse(CaseNotArchive, msgNotArchive)
	}
	return Plain, nil
}

// readManifest reads the manifest through root (util.OpenRegularFileIn: never
// through a link, never a pipe, a Windows placeholder accepted) under manifestCap,
// hashing the bytes it read, and parses them with state.LoadFrom — the snapshot
// path that resolves no store id on disk.
func readManifest(root *os.Root) (*state.Manifest, FileFacts, error) {
	f, err := util.OpenRegularFileIn(root, manifestName)
	if err != nil {
		return nil, FileFacts{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, FileFacts{}, err
	}
	h := sha256.New()
	data, err := io.ReadAll(io.TeeReader(io.LimitReader(f, manifestCap+1), h))
	if err != nil {
		return nil, FileFacts{}, err
	}
	if int64(len(data)) > manifestCap {
		return nil, FileFacts{}, errors.New("manifest over its cap")
	}
	m, err := state.LoadFrom(manifestName, data)
	if err != nil {
		return nil, FileFacts{}, err
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

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
