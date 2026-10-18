// Package instance is the per-account state of a MailArchive process that other
// launches of the same account must be able to reach and trust: the lock a
// desktop instance holds for its life, the 0600 instance file that records where
// it listens and the key K a later launch proves itself with, and the
// challenge-response handoff that makes the instance open a browser in its own
// logon session, or return a fresh link, to a caller that holds K
// (design-local-confidentiality §4.C4, P8). `serve -link` uses the same file and
// handoff [CC18].
//
// Nothing here is trusted because it is on loopback: another account can reach
// any port, so a listener is verified by a MAC over the request and over the
// reply, and the instance file lives in a directory only this account can read,
// is read without following links, and is owner-checked [CC21].
package instance

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"mail-archive-tool/internal/procs"
	"mail-archive-tool/internal/util"
)

const (
	// LockName and FileName are the desktop's lock and instance file in Dir.
	LockName = "desktop.lock"
	FileName = "desktop-instance.json"

	infoVersion  = 1
	maxInfoBytes = 8 << 10
)

// Dir is the per-account directory of the lock and the instance files: not the OS
// cache directory, which cache cleaners purge, but a place that survives them —
// %LOCALAPPDATA%\MailArchiveDesktop on Windows (not roaming, and already the
// product's directory), the product's config directory elsewhere. When it cannot
// be determined (no HOME, no %LOCALAPPDATA%) the launch refuses, visibly: it fails
// closed rather than guess [CC21].
func Dir() (string, error) {
	if runtime.GOOS == "windows" {
		la := os.Getenv("LOCALAPPDATA")
		if la == "" {
			return "", errors.New("cannot determine your per-account folder: %LOCALAPPDATA% is not set")
		}
		return filepath.Join(la, "MailArchiveDesktop"), nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine your per-account folder: %w", err)
	}
	return filepath.Join(d, "mailarchive"), nil
}

// ServeFileName is the instance file of a `serve` of the archive at out, keyed by
// a hash of its absolute path, in Dir [CC18].
func ServeFileName(out string) string {
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = out
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	sum := sha256.Sum256([]byte(abs))
	return "serve-" + hex.EncodeToString(sum[:8]) + ".json"
}

// Info is the instance file (design §4.C4 step 2). Dashboard is the address of
// the listener that answers /_handoff: the desktop's dashboard, or the `serve`
// that wrote a serve instance file.
type Info struct {
	V         int       `json:"v"`
	PID       int       `json:"pid"`
	Exe       string    `json:"exe"`
	Started   time.Time `json:"started"`
	Config    string    `json:"config,omitempty"`
	Session   string    `json:"session,omitempty"` // logon-session id, where the OS has one
	Dashboard string    `json:"dashboard"`
	Reader    string    `json:"reader,omitempty"`
	Key       string    `json:"key"` // K, 32 random bytes as 64 hex, distinct from the session's S and P
	Out       string    `json:"out,omitempty"`
	// Former records what the start found at MailArchive's former address
	// 127.0.0.1:8097 [CC22, CC23, ruling OQ-9]: "" nothing, "program" an unverified
	// listener (a program that is not MailArchive), "preupgrade" an older instance
	// that could not be ended (the Former constants). `status` reads it from here.
	Former string `json:"former,omitempty"`
	// Version is this program's build version (buildVersion()), so a launcher that
	// hands off can tell a holder of another or no version and name it, and still
	// exit 0 (§4.5.3, MA-637). Optional: a pre-SE1 instance has none.
	Version string `json:"version,omitempty"`
}

// What a desktop start found at MailArchive's former address 127.0.0.1:8097
// [CC22, CC23, ruling OQ-9]. The dashboard now takes a free port of its own, so
// nothing of this version listens there; whatever does is named, never silent.
const (
	// FormerAddress is where the dashboard used to live.
	FormerAddress = "127.0.0.1:8097"
	// FormerProgram: a listener that is not a MailArchive Desktop answers there; it
	// could be a look-alike, so a typed address or a bookmark is not to be trusted.
	FormerProgram = "program"
	// FormerPreUpgrade: an older MailArchive Desktop (one with no /_handoff) still
	// runs and could not be ended.
	FormerPreUpgrade = "preupgrade"

	// FormerProgramNotice and PreUpgradeNotice are the texts of the dashboard's
	// header and of `status` (§7.6).
	FormerProgramNotice = "Another program is using MailArchive's former address 127.0.0.1:8097. Open MailArchive only from its shortcut — a page you reach by typing that address or a bookmark may not be MailArchive."
	PreUpgradeNotice    = "An older MailArchive Desktop is still running (in this session or another account's). Do this: sign out and back in, or restart the computer, to finish the upgrade."
)

// NoticeFor is the text for a Former kind, "" for none or an unknown kind.
func NoticeFor(kind string) string {
	switch kind {
	case FormerProgram:
		return FormerProgramNotice
	case FormerPreUpgrade:
		return PreUpgradeNotice
	}
	return ""
}

// Former is what the running desktop instance of this account recorded at start
// about the former address, for `status` and the dashboard's health view: "" when
// no live instance recorded one. The file is read like any instance file, and a
// recorded kind this version does not know is ignored (§8.2).
func Former() string {
	dir, err := Dir()
	if err != nil {
		return ""
	}
	return FormerIn(dir, LogonSession())
}

// FormerIn is Former for the instance file of a session in dir.
func FormerIn(dir, session string) string {
	info, err := Read(FilePathIn(dir, session))
	if err != nil || !procs.Alive(info.PID) || NoticeFor(info.Former) == "" {
		return ""
	}
	return info.Former
}

// NewKey returns a fresh key K and its hex form.
func NewKey() (string, []byte) {
	k := make([]byte, 32)
	rand.Read(k)
	return hex.EncodeToString(k), k
}

// KeyBytes decodes the recorded key, refusing anything but 32 bytes.
func (i Info) KeyBytes() ([]byte, error) {
	k, err := hex.DecodeString(i.Key)
	if err != nil || len(k) != 32 {
		return nil, errors.New("the instance file's key is not 32 bytes of hex")
	}
	return k, nil
}

// ErrUnusable is returned (wrapped) for an instance file that exists but cannot be
// trusted: malformed, an unknown version, a bad key, or not owned by this account.
// It is treated as absent, never repaired and never believed (§8.2).
var ErrUnusable = errors.New("instance file unusable")

// Write records info at path atomically, 0600, in a directory made 0700.
func Write(path string, info Info) error {
	info.V = infoVersion
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600(path, append(b, '\n'))
}

// Read returns the instance file at path: a missing file wraps os.ErrNotExist; one
// that is a link or not a regular file, group- or other-readable on Unix, larger
// than a few KiB, malformed, of an unknown version, with a bad key, or not owned
// by this account (a SID check on Windows) wraps ErrUnusable [CC21].
func Read(path string) (Info, error) {
	data, err := util.ReadSecureFile(path, "instance file", maxInfoBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Info{}, err
		}
		return Info{}, fmt.Errorf("%w: %v", ErrUnusable, err)
	}
	if err := CheckOwner(path, OwnerReader); err != nil {
		return Info{}, fmt.Errorf("%w: %v", ErrUnusable, err)
	}
	var in Info
	if err := json.Unmarshal(data, &in); err != nil || in.V != infoVersion || in.Dashboard == "" {
		return Info{}, fmt.Errorf("%w: %s is not an instance file this version understands", ErrUnusable, filepath.Base(path))
	}
	if _, err := in.KeyBytes(); err != nil {
		return Info{}, fmt.Errorf("%w: %v", ErrUnusable, err)
	}
	return in, nil
}

// OwnerReader reads who owns path and which accounts count as this one: the owner
// (a uid on Unix, a SID on Windows) and the identifiers that are this account's.
// It is a var so a test can inject an owner without the operating system — the
// Windows check runs under test on every OS.
var OwnerReader = defaultOwnerReader

// CheckOwner refuses a file whose owner is not one of the accounts read reports as
// this one.
func CheckOwner(path string, read func(string) (owner string, accounts []string, err error)) error {
	owner, accounts, err := read(path)
	if err != nil {
		return fmt.Errorf("cannot tell who owns %s: %w", filepath.Base(path), err)
	}
	for _, a := range accounts {
		if owner == a {
			return nil
		}
	}
	return fmt.Errorf("%s is owned by %s, not by this account", filepath.Base(path), owner)
}

// MAC is HMAC-SHA256(key, label|part|part…) as hex: the proof each side of the
// handoff gives.
func MAC(key []byte, parts ...string) string {
	h := hmac.New(sha256.New, key)
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte("|"))
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SameMAC compares two hex MACs in constant time.
func SameMAC(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }
