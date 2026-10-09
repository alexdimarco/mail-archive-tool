package server

import "testing"

// covers: MA-374, R19, S22, S45
// /files/ serves exported content only: the root index.html and README.txt, and
// below them pages, .eml files and attachment zips; every operator file, dotfile
// and link is 404.
func TestFilesServesOnlyExportedContent(t *testing.T) {
	t.Skip("MA-374: /files/ has no allow-list (search.db, logs and dotfiles are served); lands in LC-C1")
}
