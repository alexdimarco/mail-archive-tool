package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// covers: MA-551
// The additive LastRun.Presync field round-trips, is nil on an older record (so
// `status` reads it as "not configured"), and an unknown field is tolerated — a
// newer writer never strands an older reader.
func TestLastRunPresyncRoundTrip(t *testing.T) {
	in := LastRun{
		Version: 2, Status: "ok", Started: time.Unix(100, 0).UTC(),
		Presync: &PresyncRecord{State: "syncing", Code: "presync_no_activity", WarnStreak: 2, Since: time.Unix(50, 0).UTC()},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"presync"`) {
		t.Fatalf("presync field not serialized: %s", b)
	}
	var got LastRun
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Presync == nil || got.Presync.Code != "presync_no_activity" || got.Presync.WarnStreak != 2 || got.Presync.State != "syncing" {
		t.Errorf("round-trip lost the presync record: %+v", got.Presync)
	}

	// An older record has no presync field -> nil ("not configured"); an unknown
	// field is ignored.
	var old LastRun
	if err := json.Unmarshal([]byte(`{"version":1,"status":"ok","future_field":"x"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Presync != nil {
		t.Errorf("an older record must carry no presync, got %+v", old.Presync)
	}
	if old.Status != "ok" {
		t.Errorf("a known field was lost alongside an unknown one: %+v", old)
	}
}
