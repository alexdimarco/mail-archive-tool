package vault

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/util"
)

// The other files whose presence marks an encrypted archive (A4, A12, A2): the
// manifest (its cleartext envelope, or records that carry units), the content
// directory, and the encrypt-existing conversion marker.
const (
	manifestName  = ".mailarchive-manifest.json"
	unitsDirName  = "units"
	convertMarker = ".mailarchive-convert.json"
)

// EncryptedMarker is THE recognition predicate (phase2a §5, P2a-C8): every
// consumer that must not treat an encrypted archive as plaintext asks it, never
// Detect (which sees only a readable vault). It reports encrypted when ANY signal is present —
// either vault file in any form (judged by Lstat, never parsed, so a damaged,
// newer, foreign or linked vault counts the same), a manifest envelope that says
// encrypted or carries the encrypted format version, a units/ directory of the
// unit store's shape, a manifest record that carries a unit, or a conversion
// marker — and why. Only the absence of every signal means plaintext: a deleted
// vault is never "not encrypted" (A15). A FOLDER of one of those names is not a
// signal — a plaintext archive's store folder can carry any name, "Units" among
// them (and a case-insensitive file system matches it as "units"). An I/O error
// that is not about the vault (a folder it cannot read at all) is returned as
// err.
func EncryptedMarker(out string) (encrypted bool, why string, err error) {
	for _, name := range []string{FileName, BackupName, convertMarker} {
		fi, lerr := os.Lstat(filepath.Join(out, name))
		switch {
		case lerr == nil && !fi.IsDir():
			if name == convertMarker {
				return true, "it holds " + name + ", which only an encrypted archive has", nil
			}
			return true, "it holds an encrypted archive's key file (" + name + ")", nil
		case lerr != nil && !errors.Is(lerr, fs.ErrNotExist):
			return false, "", lerr
		}
	}
	if isUnitStore(filepath.Join(out, unitsDirName)) {
		return true, "it holds " + unitsDirName + "/, an encrypted archive's unit store", nil
	}
	enc, why, merr := manifestSaysEncrypted(filepath.Join(out, manifestName))
	if merr != nil {
		return false, "", merr
	}
	return enc, why, nil
}

// manifestSaysEncrypted reads the manifest's first key (Save writes "version"
// first, and so does the envelope) and then scans for a unit field. A record's
// unit is the object key `"unit":"`: inside a JSON string a quote is escaped, so
// those bytes never occur in a subject or folder name.
func manifestSaysEncrypted(path string) (bool, string, error) {
	// Only a regular file is read, never through a link and never a pipe (the
	// archive index's safe read, MA-660): a manifest of another kind is not a signal.
	if fi, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) || (err == nil && !fi.Mode().IsRegular()) {
		return false, "", nil
	} else if err != nil {
		return false, "", err
	}
	f, err := util.OpenRegularFile(path)
	if err != nil {
		var nr *util.NotRegularError
		if errors.Is(err, fs.ErrNotExist) || errors.As(err, &nr) {
			return false, "", nil
		}
		return false, "", err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	head, _ := r.Peek(4 << 10)
	// The header: the first two top-level keys, read from the first 4 KiB. Save
	// writes "version" first; the envelope writes "version" then "encrypted". A
	// plaintext manifest's second key is its (large) entries, whose decode stops
	// the probe — that is fine, the unit scan below covers the rest.
	dec := json.NewDecoder(bytes.NewReader(head))
	if tok, terr := dec.Token(); terr == nil && tok == json.Delim('{') {
		for i := 0; i < 2 && dec.More(); i++ {
			key, kerr := dec.Token()
			if kerr != nil {
				break
			}
			var val json.RawMessage
			if dec.Decode(&val) != nil {
				break
			}
			var version int
			var encrypted bool
			if key == "version" && json.Unmarshal(val, &version) == nil && version >= EncryptedManifestVersion {
				return true, fmt.Sprintf("its manifest is an encrypted archive's (format %d)", version), nil
			}
			if key == "encrypted" && json.Unmarshal(val, &encrypted) == nil && encrypted {
				return true, "its manifest says it is encrypted", nil
			}
		}
	}
	// Every record of an encrypted archive carries a unit (an encrypted run refuses
	// a manifest that mixes in plaintext records), so the first records settle it:
	// the scan reads at most markerScanBytes, never a whole — possibly huge, possibly
	// hostile — manifest. A crafted manifest that hides its units past that point is
	// the downgrade A15's machine-recorded archive id refuses, not this signal.
	needle := []byte(`"unit":"`)
	buf := make([]byte, len(needle)-1+64<<10)
	kept, read := 0, 0
	for read < markerScanBytes {
		n, rerr := r.Read(buf[kept:])
		read += n
		if bytes.Contains(buf[:kept+n], needle) {
			return true, "its manifest has records stored as encrypted units", nil
		}
		// Keep the last len(needle)-1 bytes, so a needle split across reads is found.
		if total := kept + n; total >= len(needle)-1 {
			kept = copy(buf, buf[total-(len(needle)-1):total])
		} else {
			kept = total
		}
		if rerr == io.EOF {
			return false, "", nil
		}
		if rerr != nil {
			return false, "", rerr
		}
	}
	return false, "", nil
}

// markerScanBytes bounds how much of a manifest EncryptedMarker reads.
const markerScanBytes = 1 << 20

// isUnitStore reports whether dir is an encrypted archive's unit store: a real
// folder (not a link) holding a two-hex-character shard folder that holds a unit
// named by 32 hex characters starting with the shard's. A plaintext store folder
// named "units" holds folders of mail and pages, never that shape. An empty units/
// (an encrypted archive's before its first unit) is not a signal on its own; the
// vault beside it is.
func isUnitStore(dir string) bool {
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return false
	}
	shards, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, sh := range shards {
		if !sh.IsDir() || !isLowerHex(sh.Name(), 2) {
			continue
		}
		units, err := os.ReadDir(filepath.Join(dir, sh.Name()))
		if err != nil {
			continue
		}
		for _, u := range units {
			if n := u.Name(); isLowerHex(n, 32) && n[:2] == sh.Name() {
				return true
			}
		}
	}
	return false
}

func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// EncryptedAtOrAbove is the first folder, of path and every folder above it, that
// is an encrypted archive, and why — walking both the path as given and the path
// with its links resolved, so a link into an encrypted archive is seen through. ""
// when none is. err is set when it cannot tell for path itself (the folder an
// operation would write into): the caller refuses rather than read that as "not
// encrypted". A folder above it that cannot be read is not called encrypted.
func EncryptedAtOrAbove(path string) (dir, why string, err error) {
	abs, aerr := filepath.Abs(path)
	if aerr != nil {
		abs = filepath.Clean(path)
	}
	for i, start := range []string{abs, util.ResolveExisting(abs)} {
		for d := start; ; d = filepath.Dir(d) {
			enc, w, merr := EncryptedMarker(d)
			if enc {
				return d, w, nil
			}
			if merr != nil && d == start && i == 0 {
				err = fmt.Errorf("cannot tell whether %s is an encrypted archive: %w", d, merr)
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return "", "", err
}
