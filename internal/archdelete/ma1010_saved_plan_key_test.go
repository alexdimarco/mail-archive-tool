package archdelete

import (
	"errors"
	"strings"
	"testing"
)

// covers: MA-1010, R36, R37
// The saved plan's "This computer's record and key" line says what step 8 will do, as the panel's
// key line does (the delete residuals' security pass, key-use finding 3): it said "to be
// forgotten" for every recorded encrypted archive, so with no secret store the plan an approver
// reads said the key would go while the panel, and the record written after the delete, said it
// was kept; and it said the same when other recorded copies keep the key. Positive twin first:
// the last recorded copy, with the store, "to be forgotten".
func TestTheSavedPlansRecordAndKeyLineSaysWhatStep8WillDo(t *testing.T) {
	dir := encryptedClean(t)
	const key = "This computer's record and key: "
	keyPlan := func(kp KeyPlan, err error) func(string) (KeyPlan, error) {
		return func(string) (KeyPlan, error) { return kp, err }
	}
	saved := func(record func(string) (RecordRef, error), kp func(string) (KeyPlan, error)) string {
		t.Helper()
		deps := testDeps()
		deps.Record, deps.KeyPlan = record, kp
		return string(RecordText(RecordInput{Status: StatusPlanned, Plan: mustPlan(t, dir, deps)}))
	}
	others := []string{"/Volumes/Backup/Mail", "/Volumes/Old/Mail"}

	if text := saved(recordedAt(dir), keyPlan(KeyPlan{ID: testArchiveID, Last: true}, nil)); !strings.Contains(text, key+"to be forgotten\n") {
		t.Fatalf("positive twin: the last recorded copy with the store:\n%s", text)
	}
	for _, c := range []struct {
		name   string
		record func(string) (RecordRef, error)
		kp     func(string) (KeyPlan, error)
		want   string
	}{
		{"the last copy with no secret store", recordedAt(dir), keyPlan(KeyPlan{ID: testArchiveID, Last: true, NoStore: true}, nil),
			"to be kept: this computer's secret store could not be opened"},
		{"other recorded copies keep the key", recordedAt(dir), keyPlan(KeyPlan{ID: testArchiveID, Others: others}, nil),
			"this copy to be forgotten; the key is kept for 2 other recorded copies"},
		{"other copies with no secret store", recordedAt(dir), keyPlan(KeyPlan{ID: testArchiveID, NoStore: true, Others: others[:1]}, nil),
			"this copy to be forgotten; the key is kept for 1 other recorded copy"},
		{"a forget plan that cannot be had", recordedAt(dir), keyPlan(KeyPlan{}, errors.New("the record is damaged")),
			"not known: to be forgotten only if this is the last copy this computer recorded"},
		{"another record's forget plan", recordedAt(dir), keyPlan(KeyPlan{ID: "ffffffffffffffffffffffffffffffff", Last: true}, nil),
			"not known: to be forgotten only if this is the last copy this computer recorded"},
		{"no keyring", recordedAt(dir), nil,
			"not known: to be forgotten only if this is the last copy this computer recorded"},
		{"not recorded here", nil, keyPlan(KeyPlan{ID: testArchiveID, Last: true}, nil), "not applicable"},
	} {
		if text := saved(c.record, c.kp); !strings.Contains(text, key+c.want+"\n") {
			t.Errorf("%s: the saved plan should say %q:\n%s", c.name, key+c.want, text)
		}
	}
}
