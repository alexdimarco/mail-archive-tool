package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// answerThenCancel is a context that answers "not cancelled" to its first n questions and "cancelled"
// to every one after: the moment a preview is cancelled, to the record.
type answerThenCancel struct {
	context.Context
	asked atomic.Int64
	n     int64
}

func (c *answerThenCancel) Err() error {
	if c.asked.Add(1) > c.n {
		return context.Canceled
	}
	return nil
}

// covers: MA-435, MA-531, R29, S52
// A preview that is cancelled (the owner changed the selection, or left the page) stops at the next
// record, not at the next 256th (E4-AC7, E4-AO8): each record without an original reads its archived page
// whole, up to 32 MiB, and parses it twice, so 256 records of a hostile folder were minutes of work
// after the cancel, and the new preview's job started beside the old one's.
func TestArchiveMessagesStopAtTheNextRecordWhenCancelled(t *testing.T) {
	a := buildCEArchive(t)
	s := a.open()
	hello := a.key("hello")
	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = hello
	}
	// the positive twin: nobody cancels, every record is resolved
	if res, err := s.Messages(context.Background(), keys[:300], nil, nil); err != nil || len(res.Messages) != 300 {
		t.Fatalf("the positive twin: %d messages, %v", len(res.Messages), err)
	}
	ctx := &answerThenCancel{Context: context.Background(), n: 3}
	res, err := s.Messages(ctx, keys, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled preview: %v, want context.Canceled", err)
	}
	if len(res.Messages) > 4 {
		t.Errorf("the preview resolved %d records after it was cancelled at the 4th: it looks only every 256th record", len(res.Messages))
	}
}
