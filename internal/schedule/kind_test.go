package schedule

import "testing"

// covers: MA-631, R14, R18, S59
// The §4.6.5 kind taxonomy names a job from its leading arguments against the
// fixed set: verify; run-job (run-job and the legacy -job); capture (desktop
// -capture and the legacy -capture); export for any other non-empty job. The
// arguments are never treated as a path.
func TestMA631_KindTaxonomy(t *testing.T) {
	cases := []struct {
		name string
		job  []string
		want Kind
	}{
		{"verify", []string{"verify", "-out", "/a"}, KindVerify},
		{"run-job", []string{"run-job", "/a/.mailarchive-job-x.json"}, KindRunJob},
		{"legacy -job", []string{"-job", "/a/.mailarchive-job-x.json"}, KindRunJob},
		{"capture desktop -capture", []string{"desktop", "-capture", "-config", "/c", "-out", "/a"}, KindCapture},
		{"legacy -capture", []string{"-capture", "-config", "/c", "-out", "/a"}, KindCapture},
		{"export is any other non-empty job", []string{"-out", "/a", "-mode", "incremental"}, KindExport},
		{"a bare word that is not a verb is export", []string{"export", "-out", "/a"}, KindExport},
		{"desktop without -capture is not capture", []string{"desktop"}, KindExport},
		{"empty falls to export", nil, KindExport},
	}
	for _, c := range cases {
		if got := KindOf(c.job); got != c.want {
			t.Errorf("%s: KindOf(%v) = %q, want %q", c.name, c.job, got, c.want)
		}
	}
}
