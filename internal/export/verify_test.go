package export

import (
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-21
func TestVerifyEmptyAndUnresolved(t *testing.T) {
	out := t.TempDir()
	manifest, err := state.Load(filepath.Join(out, "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	exp := &Exporter{OutDir: out, Manifest: manifest, Log: log.New(io.Discard, "", 0)}

	msg := &model.Message{
		Subject:  "Test",
		Received: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		// References a cid image that has no matching attachment part.
		HTMLBody: `<p><img src="cid:missing123@x"> hello</p>`,
		Attachments: []model.Attachment{
			// A declared attachment whose content is empty (e.g. not downloaded).
			{Filename: "empty.bin", WriteTo: func(w io.Writer) (int64, error) { return 0, nil }},
		},
	}

	if _, err := exp.Export("store", []string{"Inbox"}, msg); err != nil {
		t.Fatal(err)
	}

	if exp.Stats.AttachmentsEmpty != 1 {
		t.Errorf("AttachmentsEmpty = %d, want 1", exp.Stats.AttachmentsEmpty)
	}
	if exp.Stats.UnresolvedInlineRef != 1 {
		t.Errorf("UnresolvedInlineRef = %d, want 1", exp.Stats.UnresolvedInlineRef)
	}
	if len(exp.Issues) != 2 {
		t.Fatalf("issues = %d, want 2: %+v", len(exp.Issues), exp.Issues)
	}

	// A fully-present attachment produces no issue.
	exp2 := &Exporter{OutDir: t.TempDir(), Manifest: mustManifest(t), Log: log.New(io.Discard, "", 0)}
	ok := &model.Message{
		Subject:  "Clean",
		Received: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
		HTMLBody: "<p>no images</p>",
		Attachments: []model.Attachment{
			{Filename: "doc.txt", WriteTo: func(w io.Writer) (int64, error) { n, _ := w.Write([]byte("hi")); return int64(n), nil }},
		},
	}
	if _, err := exp2.Export("store", []string{"Inbox"}, ok); err != nil {
		t.Fatal(err)
	}
	if len(exp2.Issues) != 0 || exp2.Stats.AttachmentsEmpty != 0 || exp2.Stats.UnresolvedInlineRef != 0 {
		t.Errorf("clean message produced issues: %+v", exp2.Issues)
	}
	if exp2.Stats.Attachments != 1 {
		t.Errorf("expected 1 archived attachment, got %d", exp2.Stats.Attachments)
	}
}

func mustManifest(t *testing.T) *state.Manifest {
	t.Helper()
	m, err := state.Load(filepath.Join(t.TempDir(), "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
