package health

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1158, R18
// The commands status and Status & health print name a Windows archive folder as it is typed,
// in double quotes with each backslash single (the 1.6.1 friction review's P8: the Fixity
// coverage line showed `mailarchive verify -out "C:\\Users\\…"`, Go's quoted form); a drive or
// share root keeps its closing quote (its trailing backslash doubled, as the C runtime reads it
// back). Positive twin first: a POSIX folder is named as before.
func TestStatusNamesAWindowsFolderAsItIsTyped(t *testing.T) {
	if got := outArg(Input{Out: "/Users/a/Mail Archive", GOOS: "darwin"}); got != `"/Users/a/Mail Archive"` {
		t.Fatalf("the positive twin: %s", got)
	}
	for out, want := range map[string]string{
		`C:\Users\Public\pfwlab\Archives\Walk Archive`: `"C:\Users\Public\pfwlab\Archives\Walk Archive"`,
		`E:\`:               `"E:\\"`,
		`\\nas\backup\Mail`: `"\\nas\backup\Mail"`,
	} {
		if got := outArg(Input{Out: out, GOOS: "windows"}); got != want {
			t.Errorf("outArg(%s) = %s, want %s", out, got, want)
		}
	}
	if got := shellQuoteToken(`D:\My Mail\`, "windows"); got != `"D:\My Mail\\"` {
		t.Errorf("a schedule command's token that ends in a backslash: %s", got)
	}

	in := healthyInput(gapNow)
	in.GOOS, in.Out = "windows", `C:\Users\Public\pfwlab\Archives\Walk Archive`
	in.Messages, in.WithFixity = 10, 4
	in.History = state.HistoryStat{Exists: true, TornTail: true}
	rep := Assess(in, gapNow)
	all := strings.Join(append(Summary(in, rep), rep.Reasons...), "\n")
	typed := `-out "C:\Users\Public\pfwlab\Archives\Walk Archive"`
	for _, want := range []string{"`mailarchive verify " + typed + "` to check the bytes", "`mailarchive verify -record " + typed + "` to baseline the rest", "`mailarchive reindex " + typed + "` to compact"} {
		if !strings.Contains(all, want) {
			t.Errorf("status lacks %s:\n%s", want, all)
		}
	}
	assure.Reached(t, !strings.Contains(all, `\\Users`), "no doubled backslash in any command:\n"+all)
}
