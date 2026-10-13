package desktop

import (
	"strings"
	"testing"
)

// segment is the part of src from the first occurrence of from up to, not including, the next occurrence of to
// after it ("" when from is not there).
func segment(src, from, to string) string {
	i := strings.Index(src, from)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest[len(from):], to); j >= 0 {
		return rest[:len(from)+j]
	}
	return rest
}

// covers: MA-424, MA-533, R12
// Re-verification of EW-E6's friction fix F10 (script read, not run: no browser here, E3 OW-19): a retry's preview
// locks Extract attachments, the way and the other two boxes and says a retry keeps the earlier export's choices;
// nothing unlocked them, so after Back and a new selection the boxes stayed locked, under a note that a retry
// cannot be changed, and a new export could not be made with a different choice until the page was reloaded.
// Back now gives the boxes back as they were before the retry locked them, enables the two ways only while
// Extract attachments is ticked, and hides the note about a retry's choices. And for a retry of an export made
// with Extract attachments off, the greyed radios show the first visit's "One zip per message", not "Loose
// files" (the plan's attachmentsZip is false when the option is off).
func TestBackGivesTheBoxesBackAndARetryWithTheOptionOffShowsTheDefaultWay(t *testing.T) {
	lock := segment(workspaceJS, "if(p.retryOf){\n    // a retry keeps", "\n  }\n")
	if lock == "" {
		t.Fatal("workspace.js has no block that locks the boxes for a retry")
	}
	for _, want := range []string{"st.boxes=", "$('retryChoicesNote').hidden=false",
		"if(!p.attachments)$('optAttachZip').checked=true;"} {
		if !strings.Contains(lock, want) {
			t.Errorf("the retry's lock does not do %q: it would not remember the boxes for Back, or it shows 'Loose files' for an export made without the option", want)
		}
	}
	// the saved boxes are those of the page, not of an earlier retry's lock
	if !strings.Contains(lock, "if(!st.boxes)") {
		t.Errorf("the retry's lock saves the boxes every time, so a second retry preview would save the first one's plan as the owner's choices")
	}

	unlock := segment(workspaceJS, "function unlockChoices(){", "\n}\n")
	if unlock == "" {
		t.Fatal("workspace.js has no unlockChoices: after Back the boxes stay locked")
	}
	for _, want := range []string{"['optPreserve','optHTML','optAttach'].forEach(id=>{$(id).disabled=false;})", "st.boxes.attach", "st.boxes.loose", "syncAttachMode()",
		"$('retryChoicesNote').hidden=true", "st.boxes=null"} {
		if !strings.Contains(unlock, want) {
			t.Errorf("unlockChoices does not do %q", want)
		}
	}
	if back := segment(workspaceJS, "$('backBtn').addEventListener('click',", "\n"); !strings.Contains(back, "unlockChoices()") {
		t.Errorf("Back does not unlock the boxes: %q", back)
	}
}

// covers: MA-424, MA-535, R12
// Re-verification of EW-E6's friction fix F8: when no message of the selection has a size the reader knows (a
// source that reports none), the plan adds no bytes for Extract attachments, and the Extract attachments line
// of the preview said "the size above includes them: about 0 B of it, an upper bound", a figure that is not
// an estimate of anything. The line gives the figure only when there is one.
func TestThePreviewLineGivesTheAttachmentsShareOnlyWhenThereIsOne(t *testing.T) {
	line := segment(workspaceJS, "const attInc=", ";\n")
	if line == "" {
		t.Fatal("workspace.js has no attInc")
	}
	for _, want := range []string{"p.attachBytes>0", "about '+p.attachEstimate+' of it", "an upper bound", "their size is not known"} {
		if !strings.Contains(line, want) {
			t.Errorf("the preview's attachments line lacks %q: it would say 'about 0 B of it' when every size is unknown", want)
		}
	}
}
