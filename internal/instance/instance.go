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
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
			return "", accountFolderErr(errors.New("%LOCALAPPDATA% is not set"), true)
		}
		return filepath.Join(la, "MailArchiveDesktop"), nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", accountFolderErr(err, homeUnset(runtime.GOOS, os.Getenv))
	}
	return filepath.Join(d, "mailarchive"), nil
}

// ErrNoAccountFolder is Dir's error when the per-account folder cannot be determined (no HOME, no
// %LOCALAPPDATA%, a relative XDG_CONFIG_HOME), so a caller can say so in plain words (the 1.5.3
// re-walk's N20); its text is what Dir said before.
var ErrNoAccountFolder = errors.New("cannot determine your per-account folder")

// ErrNoHomeFolder is also Dir's error when that is because no home folder is set (homeUnset), and
// only then: another cause, such as a relative XDG_CONFIG_HOME with HOME set, keeps its own words
// (the settle pass's LOW-5). Its text is never shown; Dir's error says ErrNoAccountFolder's.
var ErrNoHomeFolder = errors.New("no home folder is set")

// noHomeErr is an ErrNoAccountFolder error that is also ErrNoHomeFolder, its text unchanged.
type noHomeErr struct{ error }

func (e noHomeErr) Unwrap() error        { return e.error }
func (e noHomeErr) Is(target error) bool { return target == ErrNoHomeFolder }

// accountFolderErr is Dir's error for cause: ErrNoAccountFolder, and ErrNoHomeFolder when noHome.
func accountFolderErr(cause error, noHome bool) error {
	err := fmt.Errorf("%w: %w", ErrNoAccountFolder, cause)
	if noHome {
		return noHomeErr{err}
	}
	return err
}

// homeUnset says the folder Dir uses on goos cannot be found because no home folder is set: no
// LOCALAPPDATA on Windows, no HOME on macOS, and on Linux and the other Unixes neither
// XDG_CONFIG_HOME nor HOME (a relative XDG_CONFIG_HOME is a cause of its own).
func homeUnset(goos string, getenv func(string) string) bool {
	switch goos {
	case "windows":
		return getenv("LOCALAPPDATA") == ""
	case "darwin", "ios":
		return getenv("HOME") == ""
	}
	return getenv("HOME") == "" && getenv("XDG_CONFIG_HOME") == ""
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
	// Ended, when set, is the older instance this launch ended (§4.5.2, MA-638).
	Ended *Ended `json:"ended,omitempty"`
	// Unlabelled says Windows would not give this file the label that keeps sandboxed
	// programs from reading it, so it was written as 1.7.1 wrote it (WriteNoting): `status`
	// and Status & health say so in one line (the 1.7.2 final check's security-1).
	// Optional: an older reader ignores it.
	Unlabelled bool `json:"unlabelled,omitempty"`
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
	// FormerEnded: this launch ended an older MailArchive Desktop of this account;
	// the pid and program travel in the Ended field and the notice is dynamic, so
	// NoticeFor does not cover it (§4.5.2). An older reader ignores this kind.
	FormerEnded = "ended"

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

// Ended records an older instance this launch ended (§4.5.2): its pid and the
// program path (already control-stripped), so the dashboard header and status can
// name it. Optional; an older reader ignores it.
type Ended struct {
	PID     int    `json:"pid"`
	Program string `json:"program,omitempty"`
}

// EndedNotice is the §4.5.2 line naming the ended instance, "" for none.
func EndedNotice(e *Ended) string {
	if e == nil || e.PID == 0 {
		return ""
	}
	return fmt.Sprintf("This launch ended an older MailArchive Desktop of this account (process %d, %s). A run it was doing reads RED until the next run finishes.", e.PID, e.Program)
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
	if err != nil || !procs.Alive(info.PID) {
		return ""
	}
	// FormerEnded carries a dynamic notice (the pid/program, via Ended), so it is
	// kept although NoticeFor does not cover it; other kinds survive only with a
	// notice. An unknown kind is dropped (§8.2).
	if info.Former == FormerEnded || NoticeFor(info.Former) != "" {
		return info.Former
	}
	return ""
}

// EndedRecord is what the running desktop instance of this account recorded
// ending at start (§4.5.2), or nil when no live instance recorded one.
func EndedRecord() *Ended {
	dir, err := Dir()
	if err != nil {
		return nil
	}
	return EndedFor(dir, LogonSession())
}

// EndedFor is Ended for the instance file of a session in dir.
func EndedFor(dir, session string) *Ended {
	info, err := Read(FilePathIn(dir, session))
	if err != nil || !procs.Alive(info.PID) || info.Former != FormerEnded {
		return nil
	}
	return info.Ended
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

// RefuseSettingsWrite refuses an instance file inside an encrypted archive (design
// encrypted-archives-phase3 §6 E5): it holds the instance's key. encmark installs
// its check here (this package sits beneath it); until then it refuses nothing.
var RefuseSettingsWrite = func(path, kind string) error { return nil }

// Write records info at path atomically, 0600, in a directory made 0700; on Windows the
// file is created with a Medium integrity label that no lower integrity can read
// (writeSecret; the menu-bar S1 adversarial re-check r3, C1): K yields a Desktop session,
// so a Low-integrity process of this account (a sandbox) must not read it. When Windows
// will not set that label, the file is written as 1.7.1 wrote it (WriteNoting).
func Write(path string, info Info) error {
	_, err := WriteNoting(path, info, nil)
	return err
}

// WriteNoting is Write that tells note, in one plain sentence, when Windows would not set
// the label (util.SecretCreateRefused: a file system that keeps no labels, a network
// folder, or an account whose programs may not set it, which Microsoft's documentation
// leaves open for a person's own programs; the 1.7.2 research note's L0). The file is then
// written exactly as 1.7.1 wrote it, owner-only and unlabelled, with Unlabelled recorded in
// it, so MailArchive Desktop still starts: 1.7.2 is never worse than 1.7.1 there. An error
// that is not the label's own refusal (util.LabelRefused: a sharing violation, a full disk)
// is asked about once more first, so a passing fault does not cost the protection for the
// Desktop's whole life (the final check's security-3); when it stays and the plain write
// works, the folder can be written and only the protected write cannot, which is the same
// fallback. labelled says the file carries the label. It returns an error only when no usable
// file was written: a *KeyFileError when both writes failed.
func WriteNoting(path string, info Info, note func(string)) (labelled bool, err error) {
	if err := RefuseSettingsWrite(path, "the Desktop instance file"); err != nil {
		return false, err
	}
	info.V = infoVersion
	info.Unlabelled = false
	data, err := infoBytes(info)
	if err != nil {
		return false, err
	}
	err = writeSecret(path, data)
	dir, cause, refused := util.SecretCreateRefused(err)
	if !refused {
		return err == nil, err
	}
	if !util.LabelRefused(cause) {
		err = writeSecret(path, data)
		if err == nil {
			return true, nil
		}
		if dir, cause, refused = util.SecretCreateRefused(err); !refused {
			return false, err
		}
	}
	info.Unlabelled = true
	plain, err := infoBytes(info)
	if err != nil {
		return false, err
	}
	if perr := writePlain(path, plain); perr != nil {
		return false, &KeyFileError{Dir: dir, Protected: cause, Plain: perr}
	}
	if note != nil {
		note(UnlabelledNote(dir, cause))
	}
	return false, nil
}

// infoBytes is info as the instance file holds it.
func infoBytes(info Info) ([]byte, error) {
	b, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// RetryProtected writes info at path with its label once more, for a file the fallback wrote
// (Holder.Watch, the final check's security-3): true when the protected write worked and the
// file now carries the label; otherwise the file is left as it is.
func RetryProtected(path string, info Info) bool {
	info.V, info.Unlabelled = infoVersion, false
	data, err := infoBytes(info)
	return err == nil && writeSecret(path, data) == nil
}

// KeyFileError is WriteNoting's failure to leave any usable private key file: the protected
// write failed (Protected, the system's words) and writing it as 1.7.1 did failed too (Plain):
// the folder cannot be written at all.
type KeyFileError struct {
	Dir              string
	Protected, Plain error
}

// Error gives each write's system words only (the 1.7.2 last check's words-1).
func (e *KeyFileError) Error() string {
	return fmt.Sprintf("could not save the private key file in %s: the protected save failed (%s), and saving it the 1.7.1 way failed too (%s)", e.Dir, SystemWords(e.Protected), SystemWords(e.Plain))
}

func (e *KeyFileError) Unwrap() []error { return []error{e.Protected, e.Plain} }

// UnlabelledNote is the warning of a private key file Windows would not label: written as
// 1.7.1 wrote it, readable by any program running under this account (the final check's
// wording-10: the name the notes use, right for a `serve`'s file too, and the system's words
// without their own full stop).
func UnlabelledNote(dir string, cause error) string {
	return fmt.Sprintf("warning: Windows would not protect MailArchive's private key file in %s (%s), so it was saved as MailArchive 1.7.1 saved it: everything works as usual, but a sandboxed program running under your account may be able to read it.", dir, SystemWords(cause))
}

// SystemWords is the system's own words in err, without the full stop Windows ends its messages
// with, for a sentence that goes on after it (the final check's wording-8): a file write's error
// is unwrapped to what the system said, without the write's "temp file in …", its paths and its
// temp file's name (the last check's words-1).
func SystemWords(err error) string {
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe) && pe.Err != nil:
		err = pe.Err
	case errors.As(err, &le) && le.Err != nil:
		err = le.Err
	}
	return strings.TrimSuffix(fmt.Sprint(err), ".")
}

// UnlabelledNotice is the one plain line `status` and Status & health show while the running
// MailArchive Desktop's private key file could not be protected (security-1, wording-2).
const UnlabelledNotice = "On this computer Windows would not protect MailArchive Desktop's private key file, so it is kept as in 1.7.1: a sandboxed program running under your account may be able to read it."

// Unlabelled says the running desktop instance of this account wrote its private key file as
// 1.7.1 did (Info.Unlabelled), for `status` and Status & health; false when none runs.
func Unlabelled() bool {
	dir, err := Dir()
	if err != nil {
		return false
	}
	return UnlabelledIn(dir, LogonSession())
}

// UnlabelledIn is Unlabelled for the instance file of a session in dir.
func UnlabelledIn(dir, session string) bool {
	info, err := Read(FilePathIn(dir, session))
	return err == nil && procs.Alive(info.PID) && info.Unlabelled
}

// writeSecret is how an instance file is written: util.WriteSecretFileAtomic0600 (a var
// only so MA-1427's test can check that it is, on every OS). writePlain is how it is
// written when Windows will not label it: util.WriteFileAtomic0600, as 1.7.1 wrote it.
var (
	writeSecret = util.WriteSecretFileAtomic0600
	writePlain  = util.WriteFileAtomic0600
)

// SetWritersForTest replaces writeSecret and writePlain for a test in another package (the
// Desktop's start when Windows will not label its start file) and returns their restore.
// Production code never calls it.
func SetWritersForTest(secret, plain func(path string, data []byte) error) (restore func()) {
	oldSecret, oldPlain := writeSecret, writePlain
	writeSecret, writePlain = secret, plain
	return func() { writeSecret, writePlain = oldSecret, oldPlain }
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
