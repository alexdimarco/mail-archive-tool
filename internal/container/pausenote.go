package container

// The pause note (design-encrypted-archives-phase3 §6, E6): the reason an
// encrypted archive was paused, kept inside the sealed record because a pause
// file beside the archive would hold it in plaintext (phase2a §4). The pause
// file itself stays the empty marker.

// maxPauseNote bounds the note, as the plaintext pause file bounds its reason.
const maxPauseNote = 16 * 1024

// PauseNote is the note sealed with the record, or "" when there is none.
func (s *Store) PauseNote() string { return s.hdr.PauseNote }

// SetPauseNote records note, cut to its bound, for the next commit to seal; ""
// clears it.
func (s *Store) SetPauseNote(note string) {
	if len(note) > maxPauseNote {
		note = note[:maxPauseNote]
	}
	s.hdr.PauseNote = note
}
