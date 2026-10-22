package schedule

import (
	"encoding/json"
	"strings"
	"testing"
)

// covers: MA-551
// The additive Descriptor.Presync field round-trips, is nil on an older
// descriptor (one without the feature), and an unknown field is tolerated.
func TestDescriptorPresyncRoundTrip(t *testing.T) {
	d := Descriptor{Version: 1, Name: "x", Presync: &PresyncDesc{Kind: "thunderbird", Profile: "/p"}}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"presync"`) {
		t.Fatalf("presync not serialized: %s", b)
	}
	var got Descriptor
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Presync == nil || got.Presync.Kind != "thunderbird" || got.Presync.Profile != "/p" {
		t.Errorf("round-trip lost presync: %+v", got.Presync)
	}

	var old Descriptor
	if err := json.Unmarshal([]byte(`{"version":1,"name":"x","future_field":"y"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Presync != nil {
		t.Errorf("an older descriptor must carry no presync, got %+v", old.Presync)
	}
}

// covers: MA-569
// DescribeSpec derives the Presync descriptor from the baked job argv, so an
// installed schedule's descriptor records that it syncs Thunderbird (for status
// display and judgement only, P6); a schedule without the flag records none.
func TestDescribeSpecDerivesPresync(t *testing.T) {
	on := DescribeSpec(Spec{Name: "x", Args: []string{"-out", "/a", "-thunderbird-sync", "-thunderbird-profile", "/p"}})
	if on.Presync == nil || on.Presync.Kind != "thunderbird" || on.Presync.Profile != "/p" {
		t.Errorf("DescribeSpec did not derive the presync descriptor: %+v", on.Presync)
	}
	off := DescribeSpec(Spec{Name: "x", Args: []string{"-out", "/a"}})
	if off.Presync != nil {
		t.Errorf("a schedule without -thunderbird-sync must record no presync, got %+v", off.Presync)
	}
}
