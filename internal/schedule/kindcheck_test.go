package schedule

import (
	"strings"
	"testing"
)

func evOf(kinds ...Kind) KindEvidence {
	e := KindEvidence{Corroborated: map[Kind]bool{}}
	for _, k := range kinds {
		e.Corroborated[k] = true
	}
	return e
}

// covers: MA-631, R14, R18, S59
// CheckKind refuses a cross-kind install under the archive's name (§4.6.5c):
// trigger 1 when owner-side state corroborates a different kind, trigger 2 when
// the recorded descriptor is export and nothing is corroborated. A same-kind
// re-install passes (R14 idempotence), and verify is never guarded.
func TestMA631_CheckKindGuard(t *testing.T) {
	const out, name = "/data/archive", "mailarchive-abc"
	cases := []struct {
		desc              string
		install, recorded Kind
		ev                KindEvidence
		wantRefuse        bool
		wantMentions      string
	}{
		{"nothing corroborated, run-job allowed", KindRunJob, KindNone, evOf(), false, ""},
		{"same-kind re-install passes (R14)", KindRunJob, KindRunJob, evOf(KindRunJob), false, ""},
		{"run-job over a corroborated capture is refused", KindRunJob, KindCapture, evOf(KindCapture), true, "capture"},
		{"capture over a corroborated run-job is refused", KindCapture, KindRunJob, evOf(KindRunJob), true, "run-job"},
		{"verify is never guarded", KindVerify, KindCapture, evOf(KindCapture), false, ""},
		{"export with no corroboration is refused (trigger 2)", KindExport, KindExport, evOf(), true, "export"},
		{"export over a corroborated export passes", KindExport, KindExport, evOf(KindExport), false, ""},
		{"run-job over a recorded export, nothing corroborated, is refused", KindRunJob, KindExport, evOf(), true, "export"},
	}
	for _, c := range cases {
		err := CheckKind(c.ev, c.install, c.recorded, out, name)
		if (err != nil) != c.wantRefuse {
			t.Errorf("%s: err=%v, wantRefuse=%v", c.desc, err, c.wantRefuse)
			continue
		}
		if c.wantRefuse && !strings.Contains(err.Error(), c.wantMentions) {
			t.Errorf("%s: refusal %q does not name %q", c.desc, err, c.wantMentions)
		}
	}
}

// covers: MA-632, R14, R18, S59
// A forged descriptor Job cannot suppress the guard: trigger 1 reads only the
// owner-side evidence, so when owner-side state corroborates kind T and the
// install's kind K (taken from the forged Job) differs, CheckKind still refuses —
// no matter what kind the recorded descriptor claims.
func TestMA632_ForgedKindCannotSuppress(t *testing.T) {
	const out, name = "/data/archive", "mailarchive-abc"
	// Owner-side state says capture; the descriptor's Job is forged to each other
	// kind to try to pass a non-capture install. Every attempt is refused.
	for _, forged := range []Kind{KindRunJob, KindExport, KindCapture} {
		err := CheckKind(evOf(KindCapture), forged, forged /* recorded, forged too */, out, name)
		if forged == KindCapture {
			if err != nil {
				t.Errorf("a genuine same-kind (capture) install must pass, got %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "capture") {
			t.Errorf("a %s install forged over a corroborated capture must be refused naming capture, got %v", forged, err)
		}
	}
}
