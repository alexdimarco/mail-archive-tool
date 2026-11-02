package archdelete

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var when = time.Date(2026, 11, 1, 10, 30, 0, 0, time.FixedZone("EST", -5*3600))

// covers: MA-963, R36
func TestTheRecordNamesNoSubjectNoAddressNoFolderBelowAStore(t *testing.T) {
	const subject = "secret-merger-plans"
	const address = "alice@example.com"
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"mailbox:" + address: address}, []rec{{Path: address + "/Inbox/2024-03-01_0900_" + subject + "_ab12cd34.html"}})
	for _, f := range []string{
		address + "/Inbox/2024-03-01_0900_" + subject + "_ab12cd34.html",
		address + "/" + subject + "-folder/2019-01-01_0000_" + subject + "_99999999.html",
		address + "/" + subject + "-folder/" + subject + "-attachments.zip",
		address + "/" + subject + "-folder/" + subject + ".txt",
	} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), f)
	}
	p := mustPlan(t, dir, testDeps())
	if len(p.Unrecorded) != 2 {
		t.Fatalf("fixture: %d unrecorded", len(p.Unrecorded))
	}
	plan := string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: p}))
	record := string(RecordText(RecordInput{Status: StatusPartial, When: when, Plan: p,
		NotRemoved: []string{address + "/Inbox/2024-03-01_0900_" + subject + "_ab12cd34.html"},
		KeptFiles:  []string{address + "/" + subject + "-folder/" + subject + ".txt"}}))
	for name, text := range map[string]string{"saved plan": plan, "record": record} {
		for _, secret := range []string{subject, address, "Inbox"} {
			if strings.Contains(text, secret) {
				t.Fatalf("the %s holds %q:\n%s", name, secret, text)
			}
		}
		// "mail folder N", as the terminal numbers it (MA-1038, the 1.5.3 re-walk's N16; was "store N").
		if !strings.Contains(text, "Mail copy not recorded: mail folder 1/…/2019-01-01_0000_…_99999999.html\n") ||
			!strings.Contains(text, "Mail copy not recorded: mail folder 1/…/…-attachments.zip\n") || strings.Contains(text, "store 1") {
			t.Fatalf("the %s must name copies by store number and stem parts:\n%s", name, text)
		}
	}
	if !strings.Contains(record, "Left: mail folder 1/…/2024-03-01_0900_…_ab12cd34.html\n") || !strings.Contains(record, "Kept: mail folder 1/…/…\n") {
		t.Fatalf("record:\n%s", record)
	}
}

// covers: MA-963, R36
func TestControlCharactersCannotForgeARecordLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows folder name cannot hold a line break (the record's rule is proven on Linux and macOS)")
	}
	planted := "x\nStatus: complete\r\x1b\u0085"
	dir := smallArchive(t, filepath.Join(t.TempDir(), planted))
	p := mustPlan(t, dir, testDeps())
	text := string(RecordText(RecordInput{Status: StatusPartial, When: when, Plan: p, Computer: "pc", Account: planted, Version: "1.5.3",
		SchedulesStopped: []string{"sched" + planted}}))
	statuses := 0
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if strings.HasPrefix(l, "Status:") {
			statuses++
			if l != "Status: not fully deleted" {
				t.Fatalf("forged status line %q", l)
			}
		}
	}
	if statuses != 1 {
		t.Fatalf("%d Status lines:\n%s", statuses, text)
	}
	for _, r := range text {
		if (r < 0x20 && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("control character %U in the record:\n%q", r, text)
		}
	}
	if !strings.Contains(text, "Folder: "+filepath.Dir(dir)+string(filepath.Separator)+"x Status: complete") ||
		!strings.Contains(text, "Schedule stopped: schedx Status: complete") {
		t.Fatalf("a line break must become a space:\n%q", text)
	}
}

// covers: MA-963, R36
func TestStoreNumbersAreTheSameInThePlanAndTheRecord(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"mailbox:zeta@example.com": "zeta@example.com", "mailbox:alpha@example.com": "alpha@example.com"}, []rec{
		{Path: "zeta@example.com/2024-01-01_0000_z_11111111.html"},
		{Path: "alpha@example.com/2024-01-01_0000_a_22222222.html"},
	})
	writeFile(t, filepath.Join(dir, "zeta@example.com", "2024-01-01_0000_old_33333333.html"), "z")
	writeFile(t, filepath.Join(dir, "alpha@example.com", "2024-01-01_0000_old_44444444.html"), "a")
	p1 := mustPlan(t, dir, testDeps())
	p2 := mustPlan(t, dir, testDeps())
	if p1.StoreNumber("alpha@example.com") != 1 || p1.StoreNumber("zeta@example.com") != 2 {
		t.Fatalf("stores = %q", p1.Stores)
	}
	plan := string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: p1}))
	record := string(RecordText(RecordInput{Status: StatusComplete, When: when, Plan: p2}))
	for _, want := range []string{"mail folder 1/2024-01-01_0000_…_44444444.html", "mail folder 2/2024-01-01_0000_…_33333333.html"} {
		if !strings.Contains(plan, want) || !strings.Contains(record, want) {
			t.Fatalf("%q must be in both:\n%s\n%s", want, plan, record)
		}
	}
}

// covers: MA-963, R36
func TestTheRecordsDigestIsItsLastLineAndThePlanMatchesTheRecord(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	p := mustPlan(t, dir, testDeps())
	plan := RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: p})
	record := RecordText(RecordInput{Status: StatusComplete, When: when.Add(time.Hour), Plan: mustPlan(t, dir, testDeps()), FilesRemoved: 2})
	for _, text := range [][]byte{plan, record} {
		i := strings.LastIndex(string(text), "Record-SHA256: ")
		sum := sha256.Sum256(text[:i])
		if string(text[i:]) != "Record-SHA256: "+hex.EncodeToString(sum[:])+"\n" {
			t.Fatalf("digest line %q does not hash the bytes before it", text[i:])
		}
	}
	if !strings.HasPrefix(string(plan), "Status: planned, nothing deleted\n") {
		t.Fatalf("Status first:\n%s", plan)
	}
	// No history log: the first run is not known, never left out (§8.1 [O2-04]).
	// Said why (MA-1028, B9).
	if !strings.Contains(string(plan), "\nFirst run: not known: the archive records no history of its runs\n") || !strings.Contains(string(plan), "\nMessages: 1 (as recorded in the archive)\n") {
		t.Fatalf("captured values:\n%s", plan)
	}
	idLine := func(text []byte, key string) string {
		for _, l := range strings.Split(string(text), "\n") {
			if strings.HasPrefix(l, key+": ") {
				return l
			}
		}
		t.Fatalf("no %s line", key)
		return ""
	}
	for _, k := range []string{"Folder identity", "Manifest SHA-256"} {
		if idLine(plan, k) != idLine(record, k) {
			t.Fatalf("%s differs between the saved plan and the record", k)
		}
	}
	// After a backup in between, the digest lines differ.
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}, {Path: stemB + ".html"}})
	after := RecordText(RecordInput{Status: StatusComplete, When: when, Plan: mustPlan(t, dir, testDeps())})
	if idLine(plan, "Manifest SHA-256") == idLine(after, "Manifest SHA-256") || idLine(plan, "Folder identity") != idLine(after, "Folder identity") {
		t.Fatal("a changed manifest must change the digest line, not the identity line")
	}
}
