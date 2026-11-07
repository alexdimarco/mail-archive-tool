package desktop

import "mail-archive-tool/internal/tbrestore"

// restoreNotices are the words for each earlier restore into Thunderbird whose folder is
// left in a Local Folders and not shown (docs/design-restore-thunderbird.md §7.2
// foldcheck2 note P2: W-UNFINISHED, and an abandoned leftover's own words), read from
// restores.log in MailArchive's default settings folder at every page load — a read,
// nothing written. A log that cannot be read says nothing here (the restore says why).
// A seam only so a test can stand in for the log.
var restoreNotices = func() []string {
	p, err := tbrestore.LogPath()
	if err != nil {
		return nil
	}
	los, err := tbrestore.Leftovers(tbrestore.NewLog(p), false)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(los))
	for _, lo := range los {
		out = append(out, lo.Words)
	}
	return out
}
