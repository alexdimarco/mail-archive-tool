package archdelete

import (
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-963, R36
// The saved plan names the schedules Delete will not stop — those a descriptor
// records that MailArchive cannot confirm, and another computer's — as "not
// removed (check it yourself)" (§8.1, conformance gap 2).
func TestTheSavedPlanNamesTheSchedulesItWillNotStop(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	deps := testDeps()
	deps.Discover = FileDiscovery{}
	// Positive twin first: no descriptor, nothing "not removed".
	text := string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: mustPlan(t, dir, deps)}))
	if strings.Contains(text, "Schedule not removed") || !containsLine(text, "Schedule to stop: "+schedule.DefaultNameFor(dir)) {
		t.Fatalf("plan:\n%s", text)
	}

	other := smallArchive(t, t.TempDir())
	host, _ := os.Hostname()
	unconfirmed := schedule.DefaultNameFor(other)
	writeDescriptor(t, dir, schedule.DescriptorName, schedule.Descriptor{Version: 1, Name: unconfirmed, Interval: "daily", At: "02:00", Job: []string{"-out", other}, Host: host})
	writeDescriptor(t, dir, schedule.VerifyDescriptorName, schedule.Descriptor{Version: 1, Name: "laptop-verify", Interval: "weekly", At: "03:00", Job: []string{"-out", "/Volumes/USB/Mail"}, Host: "computer-b-not-this-one"})
	p := mustPlan(t, dir, deps)
	text = string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: p}))
	for _, want := range []string{
		"Schedule not removed (check it yourself): " + unconfirmed,
		"Schedule not removed (check it yourself): laptop-verify (on computer computer-b-not-this-one: remove it there)",
	} {
		if !containsLine(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if containsLine(text, "Schedule to stop: "+unconfirmed) {
		t.Fatalf("an unconfirmed schedule is never one to stop:\n%s", text)
	}
}
