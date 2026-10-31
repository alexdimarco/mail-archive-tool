package desktop

import (
	"encoding/json"
	"testing"
)

// covers: MA-936, R37
// Hourly with the Thunderbird sync stays, with a note (owner ruling 5 of 2026-10-31, held
// finding effects-6): where a person picks Hourly with the sync on — a create, a cadence
// edit, or the sync switched on for a schedule already hourly — the Settings reply's note
// says that on an hour with no new mail Thunderbird stays open up to 20 minutes while the
// backup waits for mail. Positive twin first: Hourly without the sync carries no note.
// Daily or weekly with the sync carries none.
func TestAnHourlyScheduleWithTheSyncSaysThunderbirdMayStayOpen(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	save := func(label, body string) string {
		t.Helper()
		var reply map[string]any
		if err := json.Unmarshal([]byte(g.post(body)), &reply); err != nil || reply["ok"] != true {
			t.Fatalf("%s: %v %v", label, reply, err)
		}
		g.recordLast()
		note, _ := reply["note"].(string)
		return note
	}
	const want = "With Hourly and the Thunderbird sync, Thunderbird stays open for up to 20 minutes in an hour with no new mail, while the backup waits for mail."

	if note := save("hourly, no sync", `{"interval":"hourly","time":"00:15","presync":false}`); note != "" {
		t.Fatalf("positive twin: hourly without the sync carries the note %q", note)
	}
	if note := save("the sync switched on, still hourly", `{"interval":"hourly","time":"00:15","presync":true}`); note != want {
		t.Errorf("the sync switched on for an hourly schedule: note %q, want %q", note, want)
	}
	if note := save("daily with the sync", `{"interval":"daily","time":"02:00"}`); note != "" {
		t.Errorf("daily with the sync carries the note %q", note)
	}
	if note := save("weekly with the sync", `{"interval":"weekly","time":"03:00"}`); note != "" {
		t.Errorf("weekly with the sync carries the note %q", note)
	}
	if note := save("back to hourly with the sync", `{"interval":"hourly","time":"00:20"}`); note != want {
		t.Errorf("hourly chosen with the sync on: note %q, want %q", note, want)
	}
}
