package container

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-752, R37
// A record must sit in the bucket its key derives: swapping two buckets' contents
// (sections that each authenticate on their own) is refused when the record loads.
func TestARecordInAnotherBucketIsRefused(t *testing.T) {
	v := state.View{Version: state.ManifestVersion, Entries: map[string]state.Record{}}
	for i := 0; i < 40; i++ {
		v.Entries[key(i)] = rec(i)
	}
	sections, n, err := encodeSections(keys[0], v, header{}, 16, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeSections(keys[0], sections, n); err != nil {
		t.Fatalf("positive twin: the sections decode: %v", err)
	}
	a, b := -1, -1
	for i := posFirst; i < len(sections); i++ {
		if len(sections[i]) > 2 {
			if a < 0 {
				a = i
			} else if b < 0 {
				b = i
			}
		}
	}
	sections[a], sections[b] = sections[b], sections[a]
	if _, _, err := decodeSections(keys[0], sections, n); !errors.Is(err, ErrRecord) {
		t.Fatalf("swapped buckets: want ErrRecord, got %v", err)
	}
}

// covers: MA-752, R37
// The bucket count doubles while the mean bucket is over its target, and every record
// still decodes from its own bucket at the new count.
func TestTheBucketCountGrowsWithTheRecord(t *testing.T) {
	v := state.View{Version: state.ManifestVersion, Entries: map[string]state.Record{}}
	pad := strings.Repeat("x", 900)
	for i := 0; i < 6000; i++ {
		r := rec(i)
		r.Path = fmt.Sprintf("s/Inbox/%s-%d.html", pad, i)
		v.Entries[key(i)] = r
	}
	sections, n, err := encodeSections(keys[0], v, header{}, 16, base)
	if err != nil {
		t.Fatal(err)
	}
	if n <= 16 {
		t.Fatalf("about 6 MB of records stayed in %d buckets", n)
	}
	total := 0
	for _, s := range sections[posFirst:] {
		total += len(s)
	}
	if total > bucketTarget*int(n) {
		t.Fatalf("mean bucket %d bytes is over the %d target at %d buckets", total/int(n), bucketTarget, n)
	}
	got, _, err := decodeSections(keys[0], sections, n)
	if err != nil || len(got.Entries) != 6000 {
		t.Fatalf("decoded %d records, %v", len(got.Entries), err)
	}
}
