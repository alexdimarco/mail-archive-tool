package health

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1144, R18, S48
// A message a mistake in MailArchive kept out of the archive (export-panic) is told in `status`
// and Status & health with its remedy, which already says the mail is fine and the original is
// untouched: the line says "the original is untouched" once, not twice (friction review of
// plain-failure-words, Windows and Mac lab 2026-11-07: "… stopped it from saving this message
// (the original is untouched); your mail is fine (the original is untouched); please report it at
// …", and the WARN line ended "… issues (the original is untouched)"). Positive twin first: a
// message the archive could not be written for still says its original is untouched.
func TestAMistakeLineSaysTheOriginalIsUntouchedOnce(t *testing.T) {
	failed := fetchFailed("local:W1")
	failed.Reason, failed.Class, failed.Detail = "export-failed", state.ClassFillable, "write failed"
	in := gapInput(failed)
	in.GapSource = map[string]string{"ann": "/mail/Inbox"}
	rep := Assess(in, gapNow)
	all := strings.Join(append(append([]string(nil), rep.Reasons...), Summary(in, rep)...), "\n")
	if !strings.Contains(all, "the original is untouched") {
		t.Fatalf("the positive twin: the write gap no longer says its original is untouched:\n%s", all)
	}

	panicked := fetchFailed("local:P1")
	panicked.Reason, panicked.Class, panicked.Detail = "export-panic", state.ClassTerminal, "renderer fault"
	in = gapInput(panicked)
	in.GapSource = map[string]string{"ann": "/mail/Inbox"}
	rep = Assess(in, gapNow)
	lines := append(append([]string(nil), rep.Reasons...), Summary(in, rep)...)
	said := 0
	for _, l := range lines {
		if n := strings.Count(l, "the original is untouched"); n > 1 {
			t.Errorf("a line says the original is untouched %d times: %q", n, l)
		}
		if strings.Contains(l, state.ReportAddress+" (the original is untouched)") {
			t.Errorf("a line repeats it after the report address: %q", l)
		}
		if strings.Contains(l, "the original is still") {
			t.Errorf("a line adds a second note about the original: %q", l)
		}
		if strings.Contains(l, state.MistakeRemedy) {
			said++
		}
	}
	assure.Reached(t, said > 0, "the mistake's remedy is still said: "+strings.Join(lines, "\n"))
}
