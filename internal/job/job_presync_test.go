package job

import (
	"encoding/json"
	"strings"
	"testing"
)

// covers: MA-551
// The additive Job.ThunderbirdSync field round-trips (presence means ON), is nil
// on an older job file (so an older GUI reads it as off), and an unknown field
// is tolerated.
func TestJobThunderbirdSyncRoundTrip(t *testing.T) {
	j := Job{Version: 1, Name: "x", Out: "/o", Mode: "incremental", ThunderbirdSync: &ThunderbirdSync{Profile: "/p"}}
	b, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"thunderbird_sync"`) {
		t.Fatalf("thunderbird_sync not serialized: %s", b)
	}
	var got Job
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.ThunderbirdSync == nil || got.ThunderbirdSync.Profile != "/p" {
		t.Errorf("round-trip lost thunderbird_sync: %+v", got.ThunderbirdSync)
	}

	var old Job
	if err := json.Unmarshal([]byte(`{"version":1,"name":"x","out":"/o","mode":"incremental","future_field":"y"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.ThunderbirdSync != nil {
		t.Errorf("an older job must carry no thunderbird_sync, got %+v", old.ThunderbirdSync)
	}
}
