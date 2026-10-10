package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
)

func summaryText(r app.Result) string {
	var buf bytes.Buffer
	printSummary(log.New(&buf, "", 0), r, true, false, "/tmp/archive")
	return buf.String()
}

// covers: MA-388, R8, S33
// A full-mode run over a message the source holds twice prints exported=4 beside
// manifest=3 indexed=3, and nothing says why 4 exports made 3 messages (g7
// friction F13). The summary now reconciles them: how many exports replaced an
// earlier copy of the same message and how many messages are new. A fill's own
// replacements are explained by "filled", and an ordinary run, where every export
// is a new message, prints no note.
func TestSummaryReconcilesExportsWithNewMessages(t *testing.T) {
	// Positive twins: every export new; a fill replacing what it exports.
	for name, r := range map[string]app.Result{
		"all new":   {Stats: export.Stats{Exported: 3}, ManifestSize: 3, Indexed: 3},
		"fill only": {Stats: export.Stats{Exported: 2, Filled: 2}, ManifestBefore: 5, ManifestSize: 5, Indexed: 5},
	} {
		if out := summaryText(r); strings.Contains(out, "replaced") {
			t.Fatalf("%s: a note about replaced copies where nothing was duplicated:\n%s", name, out)
		}
	}
	out := summaryText(app.Result{Stats: export.Stats{Exported: 4}, ManifestSize: 3, Indexed: 3})
	for _, want := range []string{"exported=4", "manifest=3", "4 exports", "3 new message(s)", "1 replaced an earlier copy"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the summary does not reconcile 4 exports with 3 messages (lacks %q):\n%s", want, out)
		}
	}
}

// covers: MA-389, R8, R18, S33
// A run whose search index holds fewer messages than the archive's manifest
// (search.db deleted or emptied, then re-created by the run: exit 0 with
// manifest=80 indexed=40, g7 friction F4) says so and names the rebuild, as
// status's index_behind does; a run whose index holds every record, or that
// turned indexing off, says nothing.
func TestSummaryWarnsWhenTheIndexIsBehindTheManifest(t *testing.T) {
	if out := summaryText(app.Result{Stats: export.Stats{Exported: 3}, ManifestSize: 3, Indexed: 3}); strings.Contains(out, "WARNING") {
		t.Fatalf("positive twin: a complete index drew a WARNING:\n%s", out)
	}
	out := summaryText(app.Result{ManifestSize: 80, Indexed: 40})
	for _, want := range []string{"WARNING", "40 of the archive's 80 messages", `mailarchive reindex -rebuild -out "/tmp/archive"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("an index behind the manifest is not flagged (lacks %q):\n%s", want, out)
		}
	}
	var buf bytes.Buffer
	printSummary(log.New(&buf, "", 0), app.Result{ManifestSize: 80}, false, false, "/tmp/archive")
	if strings.Contains(buf.String(), "WARNING") {
		t.Fatalf("-index=false run warned about its index:\n%s", buf.String())
	}
}
