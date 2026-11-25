package wizard

// Making a set's archives (docs/design-per-mailbox-archives.md §8 to §11, revision 2.2; step 3b of
// its build order): after the plan, the download question for each account that gets a new archive,
// the shared answers once, each archive's export in the plan's order, one summary, and the schedule
// once for the set.

// askSetDownloads asks the download question (§8, revision 2's C7; MA-1502) for each mailbox of the
// plan p that gets a new archive and whose account is asked about (setItem.ask: a Thunderbird IMAP
// folder a live account uses), in the plan's order, each named as the list showed it and numbered
// among them ("Account 1 of 3"). An archive brought up to date is not asked again (its default answer
// would export it again in full), nor is a row saved once. changed carries the accounts this run's
// Yes, prepare changed across the questions (MA-1267). It says which items were prepared: their
// archives run in Full mode.
func askSetDownloads(p setPlan, changed *[]string) (map[int]bool, error) {
	n := 0
	for _, it := range p.items {
		if it.ask {
			n++
		}
	}
	asks := prepAsks{n: n}
	prepared := map[int]bool{}
	for i, it := range p.items {
		if !it.ask {
			continue
		}
		ok, err := prepareThunderbirdGUI(it.row.path, asks.nextAs(it.row.listName()), changed)
		if err != nil {
			return nil, err
		}
		prepared[i] = ok
	}
	return prepared, nil
}
