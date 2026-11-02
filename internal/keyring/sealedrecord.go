package keyring

import (
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/state"
)

// ReadSealedRecord is the sealed manifest and timeline of the encrypted archive u has open, read
// as a reader's load reads them (§7.3): without the archive lock, writing nothing, under u's key
// for this one read only — no key is derived, copied or kept. A deletion record states from them
// the message count, the mail's dates and the first run of an archive whose key is open on the
// Desktop's page (design-archive-delete §8.1 [O2-05]); a locked u is ErrLocked.
func ReadSealedRecord(u *Unlocked) (*state.Manifest, []state.HistoryEvent, error) {
	k, _, err := u.acquire()
	if err != nil {
		return nil, nil, err
	}
	defer u.release()
	m, s, err := container.LoadManifest(u.out, container.LoadOptions{Key: k, ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	return m, s.History(), nil
}
