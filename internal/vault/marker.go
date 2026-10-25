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
// encrypted or carries the encrypted format version, a units/ directory, a
// manifest record that carries a unit, or a conversion marker — and why. Only
// the absence of every signal means plaintext: a deleted vault is never "not
// encrypted" (A15). An I/O error that is not about the vault (a folder it cannot
// read at all) is returned as err, for today's handling.
func EncryptedMarker(out string) (encrypted bool, why string, err error) {
	for _, name := range []string{FileName, BackupName} {
		if _, lerr := os.Lstat(filepath.Join(out, name)); lerr == nil {
			return true, "it holds an encrypted archive's key file (" + name + ")", nil
		} else if !errors.Is(lerr, fs.ErrNotExist) {
			return false, "", lerr
		}
	}
	for _, name := range []string{unitsDirName, convertMarker} {
		if _, lerr := os.Lstat(filepath.Join(out, name)); lerr == nil {
			return true, "it holds " + name + ", which only an encrypted archive has", nil
		} else if !errors.Is(lerr, fs.ErrNotExist) {
			return false, "", lerr
		}
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
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
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
