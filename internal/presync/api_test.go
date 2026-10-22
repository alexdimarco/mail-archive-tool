package presync

import (
	"strings"
	"testing"
)

// covers: MA-561
// Every WARN-class outcome has a what/why/do-this message (the text surfaced on
// the run record and in the health view); ok and note classes surface nothing.
// The consent message names the Automation remedy.
func TestWarnText(t *testing.T) {
	for _, o := range allOutcomes {
		msg, warn := WarnText(o)
		if o.Class == ClassWarn {
			if !warn || strings.TrimSpace(msg) == "" {
				t.Errorf("%s (WARN) has no message: warn=%v msg=%q", o.Name, warn, msg)
			}
			if !strings.Contains(msg, "archive ran") && !strings.Contains(msg, "no sync ran") && !strings.Contains(msg, "without") {
				t.Errorf("%s message should reassure the archive still ran: %q", o.Name, msg)
			}
		} else {
			if warn || msg != "" {
				t.Errorf("%s (%s) should surface no warning, got warn=%v msg=%q", o.Name, o.Class, warn, msg)
			}
		}
	}
	if msg, _ := WarnText(OutcomeQuitNotPermitted); !strings.Contains(msg, "Automation") {
		t.Errorf("quit-not-permitted message should name the Automation remedy: %q", msg)
	}
}
