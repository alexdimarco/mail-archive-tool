package archindex

import (
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1131, R40
// Friction P4 (settled 2026-11-07): an archive is Outlook-only when every store it records,
// read as the kind is read, is an Outlook data file (.pst, .ost, or -outlook's own export).
// One Thunderbird or Microsoft 365 store among them, no store at all, or only stores whose
// token is not a safe segment (dropped, as the kind drops them) is not.
func TestAnArchiveIsOutlookOnlyWhenEveryStoreIsAnOutlookFile(t *testing.T) {
	yes := map[string]map[string]string{
		"one .pst":         {"path:/mail/support.pst": "a"},
		"a .pst and .ost":  {"path:/mail/a.pst": "a", `path:C:\o\b.OST`: "b"},
		"-outlook's own":   {"path:/x/_outlook-pst/export.pst": "a"},
		"legacy path id":   {"/mail/a.pst": "a"},
		"an unsafe beside": {"path:/mail/a.pst": "a", "path:/mail/Inbox": "../b"},
	}
	for name, s := range yes {
		assure.Reached(t, outlookFrom(s), "Outlook-only: "+name)
	}
	no := map[string]map[string]string{
		"a Thunderbird folder beside": {"path:/mail/a.pst": "a", "path:/tb/Local Folders": "b"},
		"Microsoft 365 beside":        {"path:/mail/a.pst": "a", "mailbox:u@x": "b"},
		"no store":                    {},
		"only an unsafe token":        {"path:/mail/a.pst": "a/b"},
	}
	n := 0
	for name, s := range no {
		if outlookFrom(s) {
			t.Errorf("not Outlook-only, but said so: %s", name)
		}
		n++
	}
	assure.Reached(t, n, "the cases that are not Outlook-only")
}
