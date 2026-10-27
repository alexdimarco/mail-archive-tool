package container

// The note beside an encrypted archive (design-encrypted-archives-phase3 §13.1):
// its SHA-256 is sealed in the record's header at each keyed run, so the next one
// can tell a note someone changed or removed before it writes the note again.

// NoteDigest is the sealed SHA-256 of the note the last keyed run wrote, or "".
func (s *Store) NoteDigest() string { return s.hdr.NoteDigest }

// SetNoteDigest records digest for the next commit to seal; a value that is not 64
// lowercase hex characters is not kept.
func (s *Store) SetNoteDigest(digest string) {
	if len(digest) != 64 {
		return
	}
	for _, c := range digest {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return
		}
	}
	s.hdr.NoteDigest = digest
}
