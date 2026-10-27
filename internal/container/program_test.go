package container

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// The sealed program set (design-encrypted-archives-phase3-program §2, R6, R7; PG1).

func fullSet() ProgramSet {
	keep := true
	return ProgramSet{
		Format: ProgramFormat, Keep: &keep, Version: "v1.5.0", State: ProgramIntent,
		Files: []ProgramFile{{Name: "mailarchive-linux-amd64", SHA256: strings.Repeat("a", 64), Size: 33, Origin: OriginRelease,
			Version: "v1.5.0", Platform: "linux-amd64", Reads: 1, MissingRuns: 1, UncheckedSince: "2026-10-27T12:00:00Z"}},
		Incoming: []ProgramFile{{Name: "mailarchive-self-darwin-arm64", SHA256: strings.Repeat("b", 64), Size: 30, Origin: OriginSelf,
			Version: "", Platform: "darwin-arm64", Reads: 1}},
		Removing: []string{"SHA256SUMS-v1.4.0"},
		Note:     &ProgramNote{Current: strings.Repeat("c", 64), Incoming: strings.Repeat("d", 64)},
	}
}

// covers: MA-831, R37
// Format 1's fields are pinned: a change to any name or shape raises the format
// (R6, R7) — this test fails first.
func TestTheProgramSetsFormatIsPinned(t *testing.T) {
	b, err := json.Marshal(fullSet())
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"format":1,"keep":true,"version":"v1.5.0","state":"intent",` +
		`"files":[{"name":"mailarchive-linux-amd64","sha256":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `","size":33,"origin":"release","version":"v1.5.0","platform":"linux-amd64","reads":1,"missing_runs":1,"unchecked_since":"2026-10-27T12:00:00Z"}],` +
		`"incoming":[{"name":"mailarchive-self-darwin-arm64","sha256":"` + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + `","size":30,"origin":"self","platform":"darwin-arm64","reads":1}],` +
		`"removing":["SHA256SUMS-v1.4.0"],` +
		`"note":{"current":"` + "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" + `","incoming":"` + "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" + `"}}`
	assure.Reached(t, string(b) == want, "format 1 as pinned:\n"+string(b))
}

// covers: MA-831, R37
// A set is sealed and read back whole; with none sealed, Program says it is absent —
// "not chosen" (§2).
func TestTheProgramSetIsSealedAndReadBack(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	if _, st, err := s.Program(); st != ProgramAbsent || err != nil {
		t.Fatalf("a new archive's set: %v %v", st, err)
	}
	ps := fullSet()
	if err := s.SetProgram(ps); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	_, s = open(t, out, opts(2))
	got, st, err := s.Program()
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(ps)
	assure.Reached(t, st == ProgramOK && err == nil && bytes.Equal(a, b), "read back: "+string(a))
}

// covers: MA-831, R37
// A set this program cannot read — a newer format, or format 1 that fails to decode
// or to validate — is kept byte for byte: Program says why, SetProgram refuses, and a
// commit writes the sealed bytes back unchanged (R6, C3). The positive twin is the
// round trip above.
func TestASetThisProgramCannotReadIsKeptAsItIs(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  string
		st   ProgramStatus
	}{
		{"a newer format", `{"format":2,"keep":true,"shiny":{"a":[1,2]}}`, ProgramUnknown},
		{"an unknown field in format 1", `{"format":1,"keep":true,"shiny":1}`, ProgramInvalid},
		{"a bad name", `{"format":1,"files":[{"name":"../x","sha256":"` + strings.Repeat("a", 64) + `","size":1,"origin":"release","version":"v1.0.0","platform":"linux-amd64","reads":1}]}`, ProgramInvalid},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := fresh(t)
			m, s := open(t, out, opts(1))
			s.hdr.Program = json.RawMessage(c.raw)
			m.Save()
			m, s = open(t, out, opts(2))
			_, st, err := s.Program()
			serr := s.SetProgram(fullSet())
			s.SetPauseNote("another commit")
			if err := m.Save(); err != nil {
				t.Fatal(err)
			}
			_, s = open(t, out, opts(3))
			assure.Refused(t, 1, errText(serr), assure.Code(1), assure.Names("program"),
				assure.NoSideEffect(func() bool {
					return st == c.st && err != nil && errors.Is(serr, ErrProgramKept) && string(s.hdr.Program) == c.raw
				}))
		})
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// covers: MA-831, R37
// Validation (C2): every field of format 1 is checked — names by §3's pattern and never
// a temporary name or one ending -attachments.zip, versions exact vMAJOR.MINOR.PATCH or
// empty, the known origins, states and platforms, hex digests, sizes in bounds, a
// time where one is given, and no name twice (in any case). The positive twin is
// fullSet.
func TestTheProgramSetIsValidated(t *testing.T) {
	if err := ValidateProgram(fullSet()); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	bad := map[string]func(ps *ProgramSet){
		"format":             func(ps *ProgramSet) { ps.Format = 2 },
		"version":            func(ps *ProgramSet) { ps.Version = "v1.5" },
		"pre-release":        func(ps *ProgramSet) { ps.Version = "v1.5.0-rc1" },
		"state":              func(ps *ProgramSet) { ps.State = "half" },
		"name dot":           func(ps *ProgramSet) { ps.Files[0].Name = ".hidden" },
		"name slash":         func(ps *ProgramSet) { ps.Files[0].Name = "a/b" },
		"name temp":          func(ps *ProgramSet) { ps.Files[0].Name = "mailarchive-program-1.tmp" },
		"name attachments":   func(ps *ProgramSet) { ps.Files[0].Name = "x-attachments.zip" },
		"name long":          func(ps *ProgramSet) { ps.Files[0].Name = strings.Repeat("a", 65) },
		"sha":                func(ps *ProgramSet) { ps.Files[0].SHA256 = strings.Repeat("A", 64) },
		"size":               func(ps *ProgramSet) { ps.Files[0].Size = -1 },
		"size huge":          func(ps *ProgramSet) { ps.Files[0].Size = ProgramMaxFile + 1 },
		"origin":             func(ps *ProgramSet) { ps.Files[0].Origin = "elsewhere" },
		"file version":       func(ps *ProgramSet) { ps.Files[0].Version = "1.5.0" },
		"platform":           func(ps *ProgramSet) { ps.Files[0].Platform = "plan9-mips" },
		"release platform":   func(ps *ProgramSet) { ps.Files[0].Platform = "darwin-arm64" },
		"checksums misnamed": func(ps *ProgramSet) { ps.Files[0].Platform = PlatformChecksums },
		"self platform":      func(ps *ProgramSet) { ps.Incoming[0].Platform = "macos" },
		"reads":              func(ps *ProgramSet) { ps.Files[0].Reads = -1 },
		"missing runs":       func(ps *ProgramSet) { ps.Files[0].MissingRuns = -1 },
		"unchecked since":    func(ps *ProgramSet) { ps.Files[0].UncheckedSince = "yesterday" },
		"removing":           func(ps *ProgramSet) { ps.Removing = []string{"../x"} },
		"note":               func(ps *ProgramSet) { ps.Note.Current = "x" },
		"twice":              func(ps *ProgramSet) { ps.Files = append(ps.Files, ps.Files[0]) },
		"twice in any case": func(ps *ProgramSet) {
			f := ps.Files[0]
			f.Name = strings.ToUpper(f.Name)
			ps.Files = append(ps.Files, f)
		},
	}
	out := fresh(t)
	_, s := open(t, out, opts(1))
	for name, mutate := range bad {
		ps := fullSet()
		ps.Files = append([]ProgramFile(nil), ps.Files...)
		ps.Incoming = append([]ProgramFile(nil), ps.Incoming...)
		n := *ps.Note
		ps.Note = &n
		mutate(&ps)
		if err := ValidateProgram(ps); err == nil {
			t.Errorf("%s: a bad set was accepted", name)
		}
		// SetProgram refuses it too, keeping what it had.
		if err := s.SetProgram(ps); err == nil || len(s.hdr.Program) != 0 {
			t.Errorf("%s: SetProgram took a bad set", name)
		}
	}
}

// covers: MA-831, R37
// Versions compare by number, part by part (R2): v1.10.0 is above v1.9.0.
func TestProgramVersionsCompareByNumber(t *testing.T) {
	ok := CompareVersions("v1.10.0", "v1.9.0") > 0 && CompareVersions("v1.9.0", "v1.10.0") < 0 &&
		CompareVersions("v2.0.0", "v1.99.99") > 0 && CompareVersions("v1.4.0", "v1.4.0") == 0 &&
		CompareVersions("", "v0.0.1") < 0
	assure.Reached(t, ok, "versions compare by number")
}

// covers: MA-831, R37
// The platform a program file is for, by operating system and processor (R15):
// every Mac is macos; Windows x86-64 and Linux x86-64 and ARM are their own assets;
// anything else has no release asset.
func TestTheReleasePlatformOfEachSystem(t *testing.T) {
	ok := ReleasePlatform("darwin", "arm64") == "macos" && ReleasePlatform("darwin", "amd64") == "macos" &&
		ReleasePlatform("windows", "amd64") == "windows-amd64" && ReleasePlatform("linux", "amd64") == "linux-amd64" &&
		ReleasePlatform("linux", "arm64") == "linux-arm64" && ReleasePlatform("windows", "arm64") == "" &&
		ReleasePlatform("freebsd", "amd64") == ""
	assure.Reached(t, ok, "the release platforms")
}
