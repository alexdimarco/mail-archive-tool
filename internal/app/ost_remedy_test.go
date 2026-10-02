package app

import (
	"strings"
	"testing"
)

// covers: MA-282, R16, S19
// ostOpenRemedy is a pure function of the input path: a `.ost` source that fails
// to open/parse gets a reliable-path remedy appended to the CLI run log (go-pst
// cannot read every live Cached-Exchange `.ost`, S18/S19), naming the Outlook-app
// export and the Graph device path; any other source gets nothing. This is the CLI
// analog of the GUI's `.ost` steering (MA-57 still keeps the clean no-crash error).
func TestOSTOpenRemedy(t *testing.T) {
	ost := ostOpenRemedy(`C:\Users\x\AppData\Local\Microsoft\Outlook\x@utoronto.ca.ost`)
	if ost == "" {
		t.Fatal("a .ost open failure must carry a remedy, not a bare error")
	}
	if !strings.Contains(ost, "-outlook") {
		t.Errorf("remedy should name the Outlook-app export (-outlook): %q", ost)
	}
	if !strings.Contains(ost, "graph -auth device") {
		t.Errorf("remedy should name the Graph device path for a Microsoft 365 mailbox: %q", ost)
	}

	// Case-insensitive on the extension (Windows paths vary in case).
	if ostOpenRemedy("MAILBOX.OST") == "" {
		t.Error("remedy must match .OST case-insensitively")
	}

	// No remedy for sources go-pst / the other readers handle normally.
	for _, p := range []string{"archive.pst", "inbox.mbox", "/home/u/.thunderbird", "mailbox.ost.bak"} {
		if got := ostOpenRemedy(p); got != "" {
			t.Errorf("non-.ost source %q should get no .ost remedy, got %q", p, got)
		}
	}
}
