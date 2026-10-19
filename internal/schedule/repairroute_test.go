package schedule

import "testing"

// covers: MA-633, R14, R18, S59
// RepairRoute picks the surface that rebuilds a recorded schedule's corroborated
// kind (§4.6.5(e)): run-job → the wizard; capture → the card; an export
// descriptor → the command line; both kinds corroborated, or an uncorroborated
// run-job/capture, or a verify → no single repair.
func TestMA633_RepairRoute(t *testing.T) {
	ev := func(kinds ...Kind) KindEvidence {
		e := KindEvidence{Corroborated: map[Kind]bool{}}
		for _, k := range kinds {
			e.Corroborated[k] = true
		}
		return e
	}
	run := func(job []string, e KindEvidence) Route { return RepairRoute(Descriptor{Job: job}, "/data/a", e) }

	cases := []struct {
		name string
		got  Route
		want Route
	}{
		{"corroborated run-job → wizard", run([]string{"-job", "/x"}, ev(KindRunJob)), RouteWizard},
		{"corroborated capture → card", run([]string{"-capture"}, ev(KindCapture)), RouteCard},
		{"export descriptor, nothing corroborated → command", run([]string{"-out", "/data/a"}, ev()), RouteCommand},
		{"uncorroborated run-job → none", run([]string{"-job", "/x"}, ev()), RouteNone},
		{"both kinds corroborated → none", run([]string{"-job", "/x"}, ev(KindRunJob, KindCapture)), RouteNone},
		{"verify → none", run([]string{"verify", "-out", "/data/a"}, ev()), RouteNone},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: route = %d, want %d", c.name, c.got, c.want)
		}
	}
}
