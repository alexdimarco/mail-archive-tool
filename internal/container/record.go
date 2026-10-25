package container

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The manifest container's sections (design-encrypted-archives-phase2b §2):
// position 0 the header, 1 the capture gaps, 2..N+1 the record buckets.
const (
	posHeader = 0
	posGaps   = 1
	posFirst  = 2
	// bucketTarget is the mean plaintext bucket size the count is kept under: past
	// it the count doubles (one full re-seal). An estimate, measured in the slice.
	bucketTarget = 256 << 10
)

// Behind marks the search index as missing records since a generation (P2b-C16).
type Behind struct {
	Since uint64 `json:"since"`
}

// Gap is a stretch of go-back history that was not kept (P2b-C14): one widening
// interval of generations.
type Gap struct {
	From uint64 `json:"from"`
	To   uint64 `json:"to,omitempty"`
}

// header is section 0: the manifest's own fields that are not records, the
// per-mailbox walk stamps LastSeen is encoded against, and the state that lives
// only in the sealed record (A24, P2b-C13).
type header struct {
	Version         int                  `json:"version"`
	Stores          map[string]string    `json:"stores,omitempty"`
	CollapsedTokens map[string]bool      `json:"collapsed_tokens,omitempty"`
	SeenAt          map[string]time.Time `json:"seen_at,omitempty"`
	IndexBehind     *Behind              `json:"index_behind,omitempty"`
	HistoryGap      *Gap                 `json:"history_gap,omitempty"`
	RunDetails      json.RawMessage      `json:"run_details,omitempty"`
	PauseNote       string               `json:"pause_note,omitempty"`
}

// seenLatest is the LastSeen encoding of a record seen at its mailbox's last
// finished walk (P2b-C12): a marker instead of the time, so a steady scheduled run
// does not change every bucket. Any other LastSeen — a capture time, an older walk
// — is kept as a time, which changes only when the record does.
const seenLatest = "latest"

// sealedRecord is a record as a bucket holds it: LastSeen moved into Seen when it
// is the marker.
type sealedRecord struct {
	R    state.Record `json:"r"`
	Seen string       `json:"seen,omitempty"`
}

// tokenOf is the store token a record key starts with: the scope one walk covers.
func tokenOf(key string) string {
	t, _, _ := strings.Cut(key, "\x00")
	return t
}

// sealSeen moves r's LastSeen into the marker when it is its mailbox's walk stamp,
// or this run's stamp mid-walk (both decode to the walk stamp).
func sealSeen(key string, r state.Record, seenAt map[string]time.Time, runStamp time.Time) sealedRecord {
	if !r.LastSeen.IsZero() {
		if at, ok := seenAt[tokenOf(key)]; ok && !at.IsZero() && (r.LastSeen.Equal(at) || (!runStamp.IsZero() && r.LastSeen.Equal(runStamp))) {
			r.LastSeen = time.Time{}
			return sealedRecord{R: r, Seen: seenLatest}
		}
	}
	return sealedRecord{R: r}
}

// openSeen is sealSeen's inverse.
func openSeen(key string, s sealedRecord, seenAt map[string]time.Time) (state.Record, error) {
	r := s.R
	switch s.Seen {
	case "":
	case seenLatest:
		at, ok := seenAt[tokenOf(key)]
		if !ok || at.IsZero() {
			return r, fmt.Errorf("%w: record marked seen at its mailbox's last walk, but the mailbox has none", ErrRecord)
		}
		// Never before the record was first seen: a record captured during the last
		// finished walk and restamped by an interrupted one decodes to its capture,
		// the state a run that never saw it again would leave (FID-4).
		r.LastSeen = at
		if r.FirstSeen.After(at) {
			r.LastSeen = r.FirstSeen
		}
	default:
		return r, fmt.Errorf("%w: unknown seen marker %q", ErrRecord, s.Seen)
	}
	return r, nil
}

// encodeSections is the manifest's plaintext sections for n buckets, the count
// doubled until the mean bucket is under bucketTarget.
func encodeSections(k *vault.Key, v state.View, h header, n uint32, runStamp time.Time) ([][]byte, uint32, error) {
	if n < vault.MinBuckets {
		n = vault.MinBuckets
	}
	h.Version = v.Version
	h.Stores, h.CollapsedTokens = v.Stores, v.CollapsedTokens
	head, err := json.Marshal(h)
	if err != nil {
		return nil, 0, err
	}
	gaps, err := json.Marshal(v.CaptureGaps)
	if err != nil {
		return nil, 0, err
	}
	for {
		buckets := make([]map[string]sealedRecord, n)
		for key, r := range v.Entries {
			b, err := k.Bucket(key, n)
			if err != nil {
				return nil, 0, err
			}
			if buckets[b] == nil {
				buckets[b] = map[string]sealedRecord{}
			}
			buckets[b][key] = sealSeen(key, r, h.SeenAt, runStamp)
		}
		out := make([][]byte, 0, posFirst+int(n))
		out = append(out, head, gaps)
		total := 0
		for _, b := range buckets {
			if b == nil {
				b = map[string]sealedRecord{}
			}
			enc, err := json.Marshal(b)
			if err != nil {
				return nil, 0, err
			}
			total += len(enc)
			out = append(out, enc)
		}
		if total <= bucketTarget*int(n) || n >= vault.MaxBuckets {
			return out, n, nil
		}
		n *= 2
	}
}

// decodeSections is encodeSections' inverse: every record must sit in the bucket
// its key derives (a record moved to another bucket is refused).
func decodeSections(k *vault.Key, sections [][]byte, n uint32) (state.View, header, error) {
	var h header
	if len(sections) != posFirst+int(n) {
		return state.View{}, h, fmt.Errorf("%w: %d sections for %d buckets", ErrRecord, len(sections), n)
	}
	// The version first, leniently: a record a newer MailArchive sealed may add fields
	// the strict decode below would refuse as damage (the adversarial pass, FID-1).
	var probe struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(sections[posHeader], &probe) == nil && probe.Version > state.ManifestVersion {
		return state.View{}, h, fmt.Errorf("%w: the record's format is %d, this MailArchive reads %d", ErrNewerFormat, probe.Version, state.ManifestVersion)
	}
	if err := strictJSON(sections[posHeader], &h); err != nil {
		return state.View{}, h, fmt.Errorf("%w: header: %v", ErrRecord, err)
	}
	v := state.View{Version: h.Version, Stores: h.Stores, CollapsedTokens: h.CollapsedTokens, Entries: map[string]state.Record{}}
	if err := strictJSON(sections[posGaps], &v.CaptureGaps); err != nil {
		return state.View{}, h, fmt.Errorf("%w: capture gaps: %v", ErrRecord, err)
	}
	for i, raw := range sections[posFirst:] {
		var b map[string]sealedRecord
		if err := strictJSON(raw, &b); err != nil {
			return state.View{}, h, fmt.Errorf("%w: bucket %d: %v", ErrRecord, i, err)
		}
		for key, s := range b {
			want, err := k.Bucket(key, n)
			if err != nil {
				return state.View{}, h, fmt.Errorf("%w: bucket %d: %v", ErrRecord, i, err)
			}
			if want != uint32(i) {
				return state.View{}, h, fmt.Errorf("%w: a record sits in bucket %d, not its own %d", ErrRecord, i, want)
			}
			if _, dup := v.Entries[key]; dup {
				return state.View{}, h, fmt.Errorf("%w: a record appears twice", ErrRecord)
			}
			r, err := openSeen(key, s, h.SeenAt)
			if err != nil {
				return state.View{}, h, err
			}
			v.Entries[key] = r
		}
	}
	return v, h, nil
}

// strictJSON decodes raw into dst refusing unknown fields and trailing data.
func strictJSON(raw []byte, dst any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data")
	}
	return nil
}
