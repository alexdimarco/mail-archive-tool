package presyncjob

import (
	"context"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
)

// covers: MA-908, R32
// A sync job whose hook could not be prepared is recorded as not-set-up
// (design-presync-attended §5.3, slice AS5; C-34): SetNotSetUp installs a hook that runs
// no session, tells OnStart and OnEnd, and hands the run the outcome not-set-up — a
// warning, not synced, with its line in the caller's words — and the not-set-up run
// warning. Where the platform has no adapter it installs nothing, as SetHook does.
// Positive twin first: through the seam with a state folder, SetHook's own hook runs
// the session.
func TestNotSetUpIsRecorded(t *testing.T) {
	ran := 0
	session := func(context.Context, presync.Config) presync.Result {
		ran++
		return presync.Result{Outcome: presync.OutcomeOK}
	}
	restore := OverrideSessionForTest(session, t.TempDir())
	var opts app.Options
	if err := SetHook(&opts, HookOptions{}); err != nil || opts.BeforeRead == nil {
		t.Fatalf("positive twin: err %v, hook %v", err, opts.BeforeRead != nil)
	}
	opts.BeforeRead(context.Background(), nil)
	if ran != 1 {
		t.Fatalf("positive twin: the session ran %d times", ran)
	}
	restore()

	restore = OverrideSessionForTest(session, "")
	defer restore()
	opts = app.Options{}
	h := HookOptions{Surface: presync.SurfaceDesktop, GOOS: "darwin"}
	if err := SetHook(&opts, h); err == nil || opts.BeforeRead != nil {
		t.Fatalf("no state folder: SetHook err %v, hook %v; want the error and no hook", err, opts.BeforeRead != nil)
	}
	var started int
	var ended []app.PresyncOutcome
	h.OnStart = func() { started++ }
	h.OnEnd = func(o app.PresyncOutcome) { ended = append(ended, o) }
	SetNotSetUp(&opts, h)
	if opts.BeforeRead == nil {
		t.Fatal("SetNotSetUp installed no hook")
	}
	br := opts.BeforeRead(context.Background(), []string{"/in"})
	if ran != 1 {
		t.Errorf("a session ran (%d) although the sync could not be prepared", ran)
	}
	o := br.Outcome
	if o == nil || o.Name != "not-set-up" || o.Code != "presync_not_set_up" || o.Class != "warn" || o.Synced || o.Left != "none" ||
		!strings.Contains(o.Line, "could not be prepared") || strings.Contains(o.Line, "mailarchive") {
		t.Errorf("outcome %+v", o)
	}
	if len(br.Warnings) != 1 || br.Warnings[0].Code != "presync_not_set_up" || !strings.Contains(br.Warnings[0].Message, "archive ran") {
		t.Errorf("warnings %+v", br.Warnings)
	}
	if started != 1 || len(ended) != 1 || ended[0].Name != "not-set-up" {
		t.Errorf("OnStart %d times, OnEnd %+v", started, ended)
	}

	// No adapter: nothing installed.
	seamMu.Lock()
	supported = false
	seamMu.Unlock()
	opts = app.Options{}
	SetNotSetUp(&opts, h)
	if opts.BeforeRead != nil {
		t.Error("SetNotSetUp installed a hook where the platform has no adapter")
	}
}
