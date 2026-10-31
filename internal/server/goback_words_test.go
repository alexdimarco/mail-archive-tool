package server

import (
	"strings"
	"testing"
)

// covers: MA-940, S27
// The go-back page's own notes say what to do in words a person can act on (the wording
// pass before 1.5.2). Positive twin first: the page renders its availability note. With
// the archive's record unreadable it points at the Desktop's Status & health and a status
// command that runs (it needs -out), never "posture"; a plain archive's record is called
// its list of messages, an encrypted one's its record. (The availability notes themselves
// are pinned through the server in goback_test.go and the keyed reader's tests.)
func TestTheGobackNotesSayWhatToDo(t *testing.T) {
	render := func(p gobackPage) string {
		var b strings.Builder
		if err := gobackTmpl.Execute(&b, p); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if got := render(gobackPage{Availability: "available", AvailNote: "a note", Current: true}); !strings.Contains(got, `<div class="avail avail-available">a note</div>`) {
		t.Fatalf("positive twin: the availability note is not shown:\n%s", got)
	}
	for enc, record := range map[bool]string{false: "list of messages", true: "record"} {
		got := render(gobackPage{Availability: "available", Current: true, ManifestErr: true, Encrypted: enc})
		want := "The archive's " + record + " could not be read, so MailArchive cannot tell which messages to show. See Status &amp; health in MailArchive Desktop, or run <code>mailarchive status -out</code> with this archive's folder."
		if !strings.Contains(got, want) || strings.Contains(got, "posture") {
			t.Errorf("encrypted %v: the unreadable-record note: want %q:\n%s", enc, want, got)
		}
	}
}
