package health

import (
	"fmt"
	"sort"
	"strings"

	"mail-archive-tool/internal/state"
)

// shownAsSourceWords gives each recorded body_fallback reason in plain words
// (RV-10). The reason codes themselves stay in the text, in parentheses, for a
// reader who must look one up (the README lists them).
var shownAsSourceWords = map[string]string{
	"too-deeply-nested":    "nested too deeply to show safely",
	"unparseable":          "could not be read as formatted mail, for example a page built from frames", // no bracket: the code follows in one (MA-1161)
	"unsafe-after-reparse": "could not be made safe to show formatted",
	"too-large":            "too large to show formatted safely",
}

// shownAsSourceTotal is how many records are shown as source.
func shownAsSourceTotal(by map[string]int) int {
	n := 0
	for _, c := range by {
		n += c
	}
	return n
}

// shownAsSourceText is the one sentence every surface uses: what happened, why
// in plain words, and that there is nothing to do. "" when none.
func shownAsSourceText(by map[string]int) string {
	total := shownAsSourceTotal(by)
	if total == 0 {
		return ""
	}
	codes := make([]string, 0, len(by))
	for c, n := range by {
		if n > 0 {
			codes = append(codes, c)
		}
	}
	sort.Strings(codes)
	parts := make([]string, 0, len(codes))
	for _, c := range codes {
		why, ok := shownAsSourceWords[c]
		if !ok {
			why = "could not be shown formatted"
		}
		parts = append(parts, fmt.Sprintf("%d %s (%s)", by[c], why, c))
	}
	return fmt.Sprintf("%s %s shown as plain text instead of formatted pages — %s. Nothing is lost: the original is kept and still searchable. Nothing to do.", state.Count(total, "message", "messages"), plural(total, "is", "are"), strings.Join(parts, "; "))
}
