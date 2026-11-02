package archdelete

import (
	"errors"
	"testing"
)

// covers: MA-1010, R36, R37
// The plan's key line (§6, as §14 now builds it; slice 5's re-verification, residual 2) says what
// step 8 will do with an encrypted archive's key, as the keyring's forget plan says at the plan:
// forgotten only if the folder is removed and this is the last copy this computer recorded;
// otherwise kept for the other recorded copies, named (control characters stripped). A forget
// plan that cannot be had, or is of another record than the plan's, and no keyring at all, give
// the line that says both cases — never the unconditional "will forget". A folder this computer
// did not record forgets no key; a plain archive has no key line. The keyring is asked at the
// record's own spelling. Positive twin first: the last recorded copy.
func TestThePlansKeyLineSaysWhatStep8WillDo(t *testing.T) {
	dir := encryptedClean(t)
	const (
		last    = "If the folder is removed, this computer will forget this archive's key: this is the last copy of it this computer recorded. Any other copy of this archive will then need its password or recovery key to open."
		both    = "If the folder is removed and it is the last copy of this archive this computer recorded, this computer will forget the archive's key; otherwise the key is kept for the other copies."
		kept    = "This computer keeps this archive's key for its other recorded copies: /Volumes/Backup/Mail; /Volumes/Old/Mail copy. If the folder is removed, it forgets only this copy."
		noKey   = "This computer has not recorded this archive folder, so Delete forgets no key here."
		oldLine = "This computer will forget this archive's key. Any other copy of this archive will need its password or recovery key to open."
	)
	var asked []string
	keyPlan := func(kp KeyPlan, err error) func(string) (KeyPlan, error) {
		return func(spelling string) (KeyPlan, error) {
			asked = append(asked, spelling)
			return kp, err
		}
	}
	plan := func(record func(string) (RecordRef, error), kp func(string) (KeyPlan, error)) *DeletePlan {
		t.Helper()
		deps := testDeps()
		deps.Record, deps.KeyPlan = record, kp
		return mustPlan(t, dir, deps)
	}

	p := plan(recordedAt(dir), keyPlan(KeyPlan{ID: testArchiveID, Last: true}, nil))
	if got := p.KeyLine(); got != last {
		t.Fatalf("positive twin: the last recorded copy:\n got  %q\n want %q", got, last)
	}
	if len(asked) != 1 || asked[0] != p.Record.Spelling {
		t.Fatalf("the keyring was asked at %q; want once, at the record's spelling %q", asked, p.Record.Spelling)
	}

	for _, c := range []struct {
		name   string
		record func(string) (RecordRef, error)
		kp     func(string) (KeyPlan, error)
		want   string
	}{
		{"other recorded copies keep the key", recordedAt(dir), keyPlan(KeyPlan{ID: testArchiveID, Others: []string{"/Volumes/Backup/Mail", "/Volumes/Old/Mail\x07 copy"}}, nil), kept},
		{"another record's forget plan", recordedAt(dir), keyPlan(KeyPlan{ID: "ffffffffffffffffffffffffffffffff", Last: true}, nil), both},
		{"a forget plan that cannot be had", recordedAt(dir), keyPlan(KeyPlan{}, errors.New("the record is damaged")), both},
		{"no keyring", recordedAt(dir), nil, both},
		{"not recorded here", nil, keyPlan(KeyPlan{ID: testArchiveID, Last: true}, nil), noKey},
	} {
		if got := plan(c.record, c.kp).KeyLine(); got != c.want || got == oldLine {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
		}
	}
	if got := mustPlan(t, plainRowArchive(t, "Plain"), testDeps()).KeyLine(); got != "" {
		t.Errorf("a plain archive has no key line, got %q", got)
	}
}
