package keyring

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

// covers: MA-940, S27
// An encrypted archive's search messages name a remedy that works (the wording pass
// before 1.5.2): MailArchive Desktop's Maintenance → Rebuild search index, which works on
// an unlocked encrypted archive, and a reindex command that runs (it needs -out), with no
// backticks, which the page would show as they are. Positive twin first: each message
// still says search is unavailable, or may be missing messages.
func TestTheSearchMessagesNameARemedyThatWorks(t *testing.T) {
	const remedy = `Choose Maintenance → Rebuild search index in MailArchive Desktop (or run mailarchive reindex -rebuild -out "<this archive's folder>")`
	for label, c := range map[string]struct{ got, want string }{
		"no index":      {errNoIndex.Error(), "search is unavailable — this encrypted archive has no search index yet. " + remedy + " to build one"},
		"damaged":       {indexUnusable("the search index is damaged", errors.New("x")).Error(), "search is unavailable — the search index is damaged. " + remedy + " to rebuild it (x)"},
		"part missing":  {indexWhy(fs.ErrNotExist).Error(), "search is unavailable — part of the search index is missing. " + remedy + " to rebuild it"},
		"index behind":  {indexBehindNotice, "search may be missing messages: the search index is behind the archive. To catch it up, choose Maintenance → Rebuild search index in MailArchive Desktop, or run mailarchive reindex -rebuild -out \"<this archive's folder>\""},
	} {
		if !strings.Contains(c.got, "search is unavailable") && !strings.Contains(c.got, "search may be missing messages") {
			t.Fatalf("positive twin, %s: %q", label, c.got)
		}
		if !strings.HasSuffix(c.got, c.want) {
			t.Errorf("%s: %q\n  want it to end %q", label, c.got, c.want)
		}
		if strings.Contains(c.got, "`") {
			t.Errorf("%s: shows backticks: %q", label, c.got)
		}
	}
}
