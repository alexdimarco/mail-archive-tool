package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/server"
)

// Fixture paths: four messages spanning two senders, three folders and dates on
// both sides of a 2025-01 bound.
const (
	pathA = "store/Inbox/Clients/a.html" // bob, "invoice", 2025-07 (after the bound)
	pathB = "store/Inbox/b.html"         // carol, "invoice", 2025-02
	pathC = "store/Archive/c.html"       // bob, no "invoice", 2019
	pathD = "store/Inbox/d.html"         // bob, "invoice", 2024-12 (before the bound)
)

// buildSearchFixture writes a real search.db under a fresh -out directory and
// returns that directory. The messages are chosen so `from:bob invoice` has a
// deterministic hit set {A, D} and a 2025-01 lower bound splits D (before) from
// A (on/after).
func buildSearchFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	msg := func(subject, name, email, body string, when time.Time, attach ...string) *model.Message {
		m := &model.Message{
			Subject:     subject,
			SenderName:  name,
			SenderEmail: email,
			To:          "me@example.com",
			Received:    when,
			HTMLBody:    "<p>" + body + "</p>",
		}
		for _, a := range attach {
			m.Attachments = append(m.Attachments, model.Attachment{Filename: a})
		}
		return m
	}
	adds := []struct {
		folders []string
		m       *model.Message
		path    string
		key     string
	}{
		{[]string{"Inbox", "Clients"}, msg("Acme invoice #4471", "bob", "bob@example.com", "revised invoice for Acme attached", time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC), "invoice.pdf"), pathA, "kA"},
		{[]string{"Inbox"}, msg("Lunch invoice?", "carol", "carol@example.com", "the invoice for lunch", time.Date(2025, 2, 10, 9, 0, 0, 0, time.UTC)), pathB, "kB"},
		{[]string{"Archive"}, msg("Acme contract", "bob", "bob@example.com", "the contract is ready", time.Date(2019, 2, 1, 9, 0, 0, 0, time.UTC)), pathC, "kC"},
		{[]string{"Inbox"}, msg("Old invoice", "bob", "bob@example.com", "an old invoice from long ago", time.Date(2024, 12, 15, 9, 0, 0, 0, time.UTC)), pathD, "kD"},
	}
	for _, a := range adds {
		if err := ix.Add("store", a.folders, a.m, a.path, a.key); err != nil {
			t.Fatalf("add %s: %v", a.key, err)
		}
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runCLIOut runs the built binary and returns its exit code plus stdout and
// stderr separately — machine-mode output discipline (X8) is a stdout/stderr
// split, so the test must see the two streams apart.
func runCLIOut(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(testBin, args...)
	var outb, errb strings.Builder
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
	}
	return code, outb.String(), errb.String()
}

// cliPaths runs `search -paths ...` and returns the hit set as a sorted slice.
func cliPaths(t *testing.T, dir string, query ...string) []string {
	t.Helper()
	args := append([]string{"search", "-out", dir, "-paths"}, query...)
	code, stdout, stderr := runCLIOut(t, args...)
	if code != 0 {
		t.Fatalf("search %v exited %d: %s", query, code, stderr)
	}
	return sortedLines(stdout)
}

// apiPaths queries /api/search over the same archive and returns the hit set.
func apiPaths(t *testing.T, dir, q string) []string {
	t.Helper()
	ix, err := index.OpenReadonly(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	ts := httptest.NewServer(server.New(dir, ix))
	defer ts.Close()
	resp, err := ts.Client().Get(ts.URL + "/api/search?q=" + url.QueryEscape(q))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Results []index.Result `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, r := range out.Results {
		paths = append(paths, r.Path)
	}
	sort.Strings(paths)
	return paths
}

func sortedLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// covers: MA-101, R8, R11
// The terminal `search` verb and the serve `/api/search` box share one inline
// token grammar: from:/sender parity across the flag, the token and the API on
// a real exported fixture, and a partial `after:YYYY-MM` token applies a real
// month bound (the message before it drops out, the one on/after it stays).
func TestTerminalAndApiShareQueryGrammar(t *testing.T) {
	dir := buildSearchFixture(t)

	// Same query, three surfaces: inline token, explicit flag, and the API.
	termTokens := assure.Reached(t, cliPaths(t, dir, "from:bob", "invoice"), "token hit set")
	termFlags := cliPaths(t, dir, "-sender", "bob", "invoice")
	api := apiPaths(t, dir, "from:bob invoice")

	want := []string{pathA, pathD} // bob AND "invoice": A and D, not carol's B, not invoice-less C
	sort.Strings(want)
	for label, got := range map[string][]string{"token": termTokens, "flag": termFlags, "api": api} {
		if !equalStrings(got, want) {
			t.Errorf("%s hit set = %v, want %v", label, got, want)
		}
	}

	// A partial after:2025-01 token is a real lower bound: D (2024-12) drops,
	// A (2025-07) stays. Proven equal via the token form and the -after flag.
	bounded := assure.Reached(t, cliPaths(t, dir, "from:bob", "after:2025-01", "invoice"), "bounded token hit set")
	if contains(bounded, pathD) {
		t.Errorf("after:2025-01 did not exclude the 2024-12 message: %v", bounded)
	}
	if !contains(bounded, pathA) {
		t.Errorf("after:2025-01 excluded the 2025-07 message it should keep: %v", bounded)
	}
	boundedFlag := cliPaths(t, dir, "-sender", "bob", "-after", "2025-01", "invoice")
	if !equalStrings(boundedFlag, bounded) {
		t.Errorf("partial -after flag = %v, want the token result %v", boundedFlag, bounded)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// covers: MA-102, R8, R12
// Machine search output: -json is a valid JSON array of the result fields with
// the <mark> highlight tags stripped; -paths prints one archive-relative path
// per hit (NUL-separated with -0); in both modes stdout carries only the data
// and the match-count line goes to stderr; -json together with -paths is refused
// with a typed non-zero naming the conflict. The positive twins run first.
func TestMachineSearchOutput(t *testing.T) {
	dir := buildSearchFixture(t)

	// Positive twin 1: -json is a valid JSON array; data on stdout, count on stderr.
	code, stdout, stderr := runCLIOut(t, "search", "-out", dir, "-json", "from:bob", "invoice")
	if code != 0 {
		t.Fatalf("-json exited %d: %s", code, stderr)
	}
	var got []index.Result
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("-json stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(got) != 2 {
		t.Fatalf("-json returned %d results, want 2", len(got))
	}
	for _, r := range got {
		if r.Subject == "" || r.Path == "" || r.Folder == "" {
			t.Errorf("-json result missing fields: %+v", r)
		}
		// Check the decoded field, not the raw stdout: json escapes '<' to
		// <, so a raw-string scan for "<mark>" would never see it. After
		// Unmarshal the sentinel is decoded, so an unstripped tag shows here.
		if strings.Contains(r.Snippet, "<mark>") || strings.Contains(r.Snippet, "</mark>") {
			t.Errorf("-json snippet still carries <mark> tags: %q", r.Snippet)
		}
	}
	if !strings.Contains(strings.Join(snippetsOf(got), " "), "invoice") {
		t.Errorf("-json snippets lost the matched term: %+v", got)
	}
	if strings.Contains(stdout, "match(es)") {
		t.Errorf("-json leaked the count line onto stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "2 match(es)") {
		t.Errorf("-json count line not on stderr: %q", stderr)
	}

	// Positive twin 2: -paths prints one path per line, data-only on stdout.
	code, stdout, stderr = runCLIOut(t, "search", "-out", dir, "-paths", "from:bob", "invoice")
	if code != 0 {
		t.Fatalf("-paths exited %d: %s", code, stderr)
	}
	if got := sortedLines(stdout); !equalStrings(got, sortedPaths(pathA, pathD)) {
		t.Errorf("-paths stdout = %v, want %v", got, sortedPaths(pathA, pathD))
	}
	if strings.Contains(stdout, "match(es)") {
		t.Errorf("-paths leaked the count line onto stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "2 match(es)") {
		t.Errorf("-paths count line not on stderr: %q", stderr)
	}

	// Positive twin 3: -paths -0 is NUL-separated (xargs -0), never newline.
	code, stdout, stderr = runCLIOut(t, "search", "-out", dir, "-paths", "-0", "from:bob", "invoice")
	if code != 0 {
		t.Fatalf("-paths -0 exited %d: %s", code, stderr)
	}
	if strings.Contains(stdout, "\n") {
		t.Errorf("-paths -0 stdout contains a newline separator: %q", stdout)
	}
	var nulParts []string
	for _, p := range strings.Split(stdout, "\x00") {
		if p != "" {
			nulParts = append(nulParts, p)
		}
	}
	sort.Strings(nulParts)
	if !equalStrings(nulParts, sortedPaths(pathA, pathD)) {
		t.Errorf("-paths -0 parts = %v, want %v", nulParts, sortedPaths(pathA, pathD))
	}

	// The refusal: -json and -paths together is a typed non-zero naming both.
	code, _, stderr = runCLIOut(t, "search", "-out", dir, "-json", "-paths", "from:bob", "invoice")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-json", "-paths"))
}

func snippetsOf(rs []index.Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Snippet)
	}
	return out
}

func sortedPaths(p ...string) []string {
	out := append([]string{}, p...)
	sort.Strings(out)
	return out
}

// covers: MA-161, R8, R19, S22
// The plain (human) `search` output control-scrubs every mail-derived field
// before it reaches the terminal: a message whose subject, body and sender carry
// raw ESC/BEL/other C0-C1 controls prints with no control byte other than the
// printer's own newlines, so a hostile email cannot inject an ANSI/OSC escape
// sequence into the operator's terminal. The fields are scrubbed, not dropped —
// their visible text still prints.
func TestSearchPlainOutputScrubsControlChars(t *testing.T) {
	dir := t.TempDir()
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	// A hostile email: CSI erase-line + colour in the subject, an OSC window-title
	// set with a BEL terminator, a conceal sequence next to the searchable term in
	// the body, and control bytes in the sender name.
	hostile := &model.Message{
		Subject:     "invoice \x1b[2K\x1b[1;31mPWNED-SUBJECT\x1b[0m \x1b]0;hijacked\x07",
		SenderName:  "e\x1bvil \x07sender",
		SenderEmail: "evil@example.com",
		To:          "me@example.com",
		Received:    time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC),
		HTMLBody:    "<p>the quarterly \x1b[8mHIDDEN\x1b[0m invoice \x1b]0;evil\x07 report</p>",
	}
	if err := ix.Add("store", []string{"Inbox"}, hostile, "store/Inbox/h.html", "kHostile"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCLIOut(t, "search", "-out", dir, "invoice")
	if code != 0 {
		t.Fatalf("search exited %d: %s", code, stderr)
	}
	// Not one control rune reaches the terminal except the printer's own newlines.
	for i, r := range stdout {
		if r == '\n' {
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			t.Fatalf("plain search output carries control rune %#U at offset %d:\n%q", r, i, stdout)
		}
	}
	// The scrubbed subject's visible text still prints (scrubbed, not dropped).
	if !strings.Contains(stdout, "PWNED-SUBJECT") {
		t.Errorf("scrubbed subject text missing from output:\n%q", stdout)
	}

	// Positive twin: a clean message prints its subject text intact and unaltered.
	clean := t.TempDir()
	cx, err := index.Open(filepath.Join(clean, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	good := &model.Message{Subject: "Acme invoice #4471", SenderName: "bob", SenderEmail: "bob@example.com",
		To: "me@example.com", Received: time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC), HTMLBody: "<p>the invoice is attached</p>"}
	if err := cx.Add("store", []string{"Inbox"}, good, "store/Inbox/g.html", "kClean"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Close(); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runCLIOut(t, "search", "-out", clean, "invoice")
	if code != 0 {
		t.Fatalf("clean search exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Acme invoice #4471") {
		t.Errorf("clean subject not printed intact:\n%q", stdout)
	}
}
