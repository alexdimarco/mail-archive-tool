package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// randomID is n random hex characters (n even or odd).
func randomID(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		panic("desktop: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b)[:n]
}

// decodeJSONBody decodes the request body into v. An empty body is the zero value; the
// body is read through the caller's MaxBytesReader, so a body over the bound is a
// *http.MaxBytesError.
func decodeJSONBody(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(r.Body).Decode(v)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// StateDir is the per-user, LOCAL, non-roaming state directory the extraction workspace
// keeps its run records, journal and fingerprint ledger in (design §4.16, XC3): Windows
// %LOCALAPPDATA%\MailArchive, macOS ~/Library/Application Support/MailArchive, Linux
// $XDG_STATE_HOME/mailarchive (~/.local/state/mailarchive). It is not the roaming
// %AppData% config directory, which a folder-redirected RDS or VDI profile shares across
// hosts. "" when it cannot be determined: the workspace then previews but does not export.
func StateDir() string {
	switch runtime.GOOS {
	case "windows":
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			return filepath.Join(la, "MailArchive")
		}
		return ""
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "MailArchive")
		}
		return ""
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "mailarchive")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "mailarchive")
	}
	return ""
}
