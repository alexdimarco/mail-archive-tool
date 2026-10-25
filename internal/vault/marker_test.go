package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plaintextManifest is a plaintext archive's manifest whose record's subject and
// folder hold the text of every manifest signal, as JSON escapes it.
func plaintextManifest(t *testing.T) []byte {
	t.Helper()
	tricky := `"unit":"0123456789abcdef0123456789abcdef" "encrypted":true "version":100 units/`
	b, err := json.Marshal(map[string]any{
		"version": 9,
		"entries": map[string]any{"<a@b>": map[string]any{"path": "Inbox/a.html", "folder": tricky, "subject": tricky}},
		"stores":  map[string]string{"path:/mail/units": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeIn(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-727, R37
func TestEncryptedMarkerSeesEachSignalAlone(t *testing.T) {
	// The positive twin first: a plaintext archive — even one whose subject spells
	// every signal — and an empty folder are not encrypted.
	for label, setup := range map[string]func(dir string){
		"an empty folder": func(string) {},
		"a plaintext archive spelling every signal": func(dir string) { writeIn(t, dir, manifestName, plaintextManifest(t)) },
		// A plaintext archive's store folder can carry any name: one called Units (which
		// a case-insensitive file system also finds as "units"), an empty units/, and
		// folders named like the vault or the conversion marker are not signals; nor is
		// a manifest that is a link (it is never followed).
		"a plaintext store named Units": func(dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "Units", "Inbox"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeIn(t, filepath.Join(dir, "Units", "Inbox"), "2025-03-03_0900_Hello_ab12cd34.html", []byte("<p>mail</p>"))
		},
		"an empty units folder": func(dir string) { os.Mkdir(filepath.Join(dir, unitsDirName), 0o755) },
		"folders named like the vault and the marker": func(dir string) {
			for _, n := range []string{FileName, BackupName, convertMarker} {
				os.Mkdir(filepath.Join(dir, n), 0o755)
			}
		},
		"a manifest that is a link to an encrypted one": func(dir string) {
			other := t.TempDir()
			writeIn(t, other, manifestName, []byte(`{"version":100}`))
			os.Symlink(filepath.Join(other, manifestName), filepath.Join(dir, manifestName))
		},
	} {
		dir := t.TempDir()
		setup(dir)
		if enc, why, err := EncryptedMarker(dir); enc || err != nil {
			t.Fatalf("%s: read as encrypted (%q, %v)", label, why, err)
		}
	}

	cases := map[string]struct {
		setup func(t *testing.T, dir string)
		says  string
	}{
		"the vault":       {func(t *testing.T, dir string) { writeIn(t, dir, FileName, []byte("damaged")) }, FileName},
		"only its mirror": {func(t *testing.T, dir string) { writeIn(t, dir, BackupName, nil) }, BackupName},
		"a vault that is a dangling link": {func(t *testing.T, dir string) {
			if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, FileName)); err != nil {
				t.Skip("no symlinks here:", err)
			}
		}, FileName},
		"units/ with the vault deleted": {func(t *testing.T, dir string) {
			shard := filepath.Join(dir, unitsDirName, "ab")
			if err := os.MkdirAll(shard, 0o700); err != nil {
				t.Fatal(err)
			}
			writeIn(t, shard, "ab"+strings.Repeat("0", 30), []byte("sealed"))
		}, unitsDirName},
		"a conversion marker": {func(t *testing.T, dir string) { writeIn(t, dir, convertMarker, []byte("{}")) }, convertMarker},
		"the encrypted manifest format": {func(t *testing.T, dir string) {
			writeIn(t, dir, manifestName, []byte(`{"version":100,"entries":{}}`))
		}, "format 100"},
		"an envelope that says encrypted": {func(t *testing.T, dir string) {
			writeIn(t, dir, manifestName, []byte(`{"version":1,"encrypted":true}`))
		}, "says it is encrypted"},
		"a record stored as a unit": {func(t *testing.T, dir string) {
			writeIn(t, dir, manifestName, []byte(`{"version":9,"entries":{"<a@b>":{"path":"Inbox/a.html","unit":"0123456789abcdef0123456789abcdef"}}}`))
		}, "encrypted units"},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			c.setup(t, dir)
			enc, why, err := EncryptedMarker(dir)
			if err != nil || !enc {
				t.Fatalf("not recognised as encrypted (%v)", err)
			}
			if !strings.Contains(why, c.says) {
				t.Fatalf("the reason %q does not name what showed it (%q)", why, c.says)
			}
		})
	}
}

// covers: MA-727, R37
func TestEncryptedMarkerFindsAUnitSplitAcrossReadsAndReadsABoundedPrefix(t *testing.T) {
	needle := `"unit":"`
	for cut := 1; cut < len(needle); cut++ {
		// The first read is the 64 KiB the reader buffered; the needle straddles it.
		pad := (64 << 10) - cut - len(`{"version":9,"entries":{"x":{"p":"`) - len(`","`) + 1
		body := `{"version":9,"entries":{"x":{"p":"` + strings.Repeat("a", pad) + `",` + needle + `0123456789abcdef0123456789abcdef"}}}`
		if i := strings.Index(body, needle); i >= 64<<10 || i+len(needle) <= 64<<10 {
			t.Fatalf("cut %d: the needle at %d does not straddle the first read", cut, i)
		}
		dir := t.TempDir()
		writeIn(t, dir, manifestName, []byte(body))
		if enc, _, err := EncryptedMarker(dir); !enc || err != nil {
			t.Fatalf("a unit split %d bytes into the second read was missed (%v)", cut, err)
		}
	}

	// A unit past the bound is not read for: the scan never reads a whole manifest.
	var b bytes.Buffer
	b.WriteString(`{"version":9,"entries":{"x":{"p":"`)
	b.WriteString(strings.Repeat("a", markerScanBytes+(128<<10)))
	b.WriteString(`","unit":"0123456789abcdef0123456789abcdef"}}}`)
	dir := t.TempDir()
	writeIn(t, dir, manifestName, b.Bytes())
	if enc, _, err := EncryptedMarker(dir); enc || err != nil {
		t.Fatalf("the scan read past its %d-byte bound (%v)", markerScanBytes, err)
	}
}
