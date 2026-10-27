package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The program set (design-encrypted-archives-phase3-program §2, R6, R7): what the
// archive's program/ folder should hold — the release copies of MailArchive and this
// computer's own — sealed in the record's header as its own object with its own
// format. The header keeps it as raw bytes: a program that does not know the format,
// or cannot read it, writes those bytes back unchanged and leaves program/ alone.

// ProgramFormat is the format of the program set this program reads and writes. Any
// new field raises it (R6), and TestTheProgramSetsFormatIsPinned fails first.
const ProgramFormat = 1

// ProgramMaxFile is the largest program file the set may name, as the keyless check
// counts a larger one odd (§3).
const ProgramMaxFile = 256 << 20

// The states of a set: an update under way, or one that landed (§4.3).
const (
	ProgramIntent = "intent"
	ProgramLanded = "landed"
)

// The origins of a kept copy: a release's asset, or this computer's own program.
const (
	OriginRelease = "release"
	OriginSelf    = "self"
)

// ProgramSet is format 1 of the sealed set.
type ProgramSet struct {
	Format   int           `json:"format"`
	Keep     *bool         `json:"keep,omitempty"`    // the person's choice; nil: not chosen
	Version  string        `json:"version,omitempty"` // the high-water mark: the highest release version that landed
	State    string        `json:"state,omitempty"`
	Files    []ProgramFile `json:"files,omitempty"`
	Incoming []ProgramFile `json:"incoming,omitempty"` // during an intent: what replaces or joins
	Removing []string      `json:"removing,omitempty"` // during an intent: what goes
	Note     *ProgramNote  `json:"note,omitempty"`     // during an intent: the note's digests (R5)
}

// ProgramFile is one kept copy.
type ProgramFile struct {
	Name           string `json:"name"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	Origin         string `json:"origin"`
	Version        string `json:"version,omitempty"` // a release copy's version; empty for a self-copy of a development build
	Platform       string `json:"platform"`
	Reads          int    `json:"reads"`                     // the record-format revision its program opens (C4)
	MissingRuns    int    `json:"missing_runs,omitempty"`    // keyed runs in a row it was missing
	UncheckedSince string `json:"unchecked_since,omitempty"` // when it was last checkable, if it is kept online only (R4)
}

// ProgramNote is the note's digest before and after an update (R5).
type ProgramNote struct {
	Current  string `json:"current,omitempty"`
	Incoming string `json:"incoming,omitempty"`
}

// ProgramStatus is how the sealed set reads.
type ProgramStatus int

const (
	ProgramAbsent  ProgramStatus = iota // no set: the person has not chosen
	ProgramOK                           // a set of this format, valid
	ProgramUnknown                      // a format this program does not know: kept as it is
	ProgramInvalid                      // this format, but it does not decode or validate: kept as it is
)

// ErrProgramKept refuses a change to a sealed set this program cannot read: its bytes
// are kept as they are.
var ErrProgramKept = errors.New("the archive's program set is one this MailArchive cannot read; it is kept as it is")

// Program reads the sealed set. A set this program cannot read is ProgramUnknown or
// ProgramInvalid, with why.
func (s *Store) Program() (ProgramSet, ProgramStatus, error) {
	return decodeProgram(s.hdr.Program)
}

// SetProgram records ps for the next commit to seal. It refuses a set that does not
// validate, and refuses (ErrProgramKept) to replace a sealed set this program cannot
// read.
func (s *Store) SetProgram(ps ProgramSet) error {
	if _, st, err := decodeProgram(s.hdr.Program); st == ProgramUnknown || st == ProgramInvalid {
		return fmt.Errorf("%w: %v", ErrProgramKept, err)
	}
	if err := ValidateProgram(ps); err != nil {
		return err
	}
	raw, err := json.Marshal(ps)
	if err != nil {
		return err
	}
	s.hdr.Program = raw
	return nil
}

func decodeProgram(raw json.RawMessage) (ProgramSet, ProgramStatus, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return ProgramSet{}, ProgramAbsent, nil
	}
	var f struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return ProgramSet{}, ProgramInvalid, fmt.Errorf("the program set does not decode: %w", err)
	}
	if f.Format != ProgramFormat {
		return ProgramSet{}, ProgramUnknown, fmt.Errorf("the program set is format %d; this MailArchive reads %d", f.Format, ProgramFormat)
	}
	var ps ProgramSet
	if err := strictJSON(raw, &ps); err != nil {
		return ProgramSet{}, ProgramInvalid, fmt.Errorf("the program set does not decode: %w", err)
	}
	if err := ValidateProgram(ps); err != nil {
		return ProgramSet{}, ProgramInvalid, err
	}
	return ps, ProgramOK, nil
}

var (
	// programName is §3's pattern for a name in program/.
	programName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	exactSemver = regexp.MustCompile(`^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)
	// selfPlatform is <os>-<arch> of a self-copy.
	selfPlatform = regexp.MustCompile(`^(darwin|windows|linux|freebsd|openbsd|netbsd|dragonfly|illumos|solaris|aix)-(amd64|arm64|386|arm|universal|riscv64|ppc64|ppc64le|s390x|mips64|mips64le|loong64)$`)
	sha256Hex    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// releasePlatforms are the platforms a release publishes an asset for (R15).
var releasePlatforms = map[string]bool{"macos": true, "windows-amd64": true, "linux-amd64": true, "linux-arm64": true}

// ReleasePlatform is the release platform of a system (R15): every Mac is macos;
// Windows x86-64, Linux x86-64 and Linux ARM are their own; anything else has none.
func ReleasePlatform(goos, goarch string) string {
	switch p := goos + "-" + goarch; {
	case goos == "darwin":
		return "macos"
	case releasePlatforms[p]:
		return p
	}
	return ""
}

// ProgramNameOK says name may be kept in program/ (§3): the pattern, and never a
// temporary name or one ending -attachments.zip.
func ProgramNameOK(name string) bool {
	l := strings.ToLower(name)
	return programName.MatchString(name) && !strings.HasSuffix(l, "-attachments.zip") && !strings.HasSuffix(l, ".tmp")
}

// ExactVersion says v is an exact vMAJOR.MINOR.PATCH.
func ExactVersion(v string) bool { return exactSemver.MatchString(v) }

// CompareVersions compares two exact versions by number, part by part (R2); an empty
// or inexact one is below every exact one.
func CompareVersions(a, b string) int {
	pa, oka := versionParts(a)
	pb, okb := versionParts(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) ([3]int, bool) {
	var p [3]int
	if !ExactVersion(v) {
		return p, false
	}
	for i, s := range strings.SplitN(v[1:], ".", 3) {
		p[i], _ = strconv.Atoi(s)
	}
	return p, true
}

// ValidateProgram checks every field of a format-1 set (C2).
func ValidateProgram(ps ProgramSet) error {
	bad := func(what string, v any) error {
		return fmt.Errorf("the program set's %s is not valid: %q", what, fmt.Sprint(v))
	}
	if ps.Format != ProgramFormat {
		return bad("format", ps.Format)
	}
	if ps.Version != "" && !ExactVersion(ps.Version) {
		return bad("version", ps.Version)
	}
	if ps.State != "" && ps.State != ProgramIntent && ps.State != ProgramLanded {
		return bad("state", ps.State)
	}
	for _, list := range [][]ProgramFile{ps.Files, ps.Incoming} {
		seen := map[string]bool{}
		for _, f := range list {
			if err := validateProgramFile(f); err != nil {
				return err
			}
			if l := strings.ToLower(f.Name); seen[l] {
				return bad("file named twice", f.Name)
			} else {
				seen[l] = true
			}
		}
	}
	for _, n := range ps.Removing {
		if !ProgramNameOK(n) {
			return bad("name removed", n)
		}
	}
	if ps.Note != nil {
		for _, d := range []string{ps.Note.Current, ps.Note.Incoming} {
			if d != "" && !sha256Hex.MatchString(d) {
				return bad("note digest", d)
			}
		}
	}
	return nil
}

func validateProgramFile(f ProgramFile) error {
	bad := func(what string, v any) error {
		return fmt.Errorf("the program set's %s of %q is not valid: %q", what, f.Name, fmt.Sprint(v))
	}
	switch {
	case !ProgramNameOK(f.Name):
		return bad("name", f.Name)
	case !sha256Hex.MatchString(f.SHA256):
		return bad("checksum", f.SHA256)
	case f.Size < 0 || f.Size > ProgramMaxFile:
		return bad("size", f.Size)
	case f.Version != "" && !ExactVersion(f.Version):
		return bad("version", f.Version)
	case f.Origin == OriginRelease && !releasePlatforms[f.Platform]:
		return bad("platform", f.Platform)
	case f.Origin == OriginSelf && !selfPlatform.MatchString(f.Platform):
		return bad("platform", f.Platform)
	case f.Origin != OriginRelease && f.Origin != OriginSelf:
		return bad("origin", f.Origin)
	case f.Reads < 0 || f.MissingRuns < 0:
		return bad("counts", fmt.Sprint(f.Reads, " ", f.MissingRuns))
	}
	if f.UncheckedSince != "" {
		if _, err := time.Parse(time.RFC3339, f.UncheckedSince); err != nil {
			return bad("time", f.UncheckedSince)
		}
	}
	return nil
}
