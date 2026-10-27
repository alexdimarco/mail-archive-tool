package app

import (
	"path/filepath"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/util"
)

// checkProgramFolder is verify -full's comparison of the program folder and the note
// with what the record seals (program design §6, C18; PG4): read-only — nothing is
// written, swept or renamed — and every finding a warning that never changes the
// verdict on the mail.
func checkProgramFolder(out string, set *container.ProgramSet, noteDigest string) []string {
	var found []string
	if b, err := util.ReadRegularFile(filepath.Join(out, "README.txt"), maxNoteRead); noteDigest != "" && (err != nil || NoteDigest(b) != noteDigest) {
		found = append(found, "the note beside the archive (README.txt) is not the one the last backup wrote — do not follow it; the next backup writes it again")
	}
	if set == nil || set.Keep == nil || !*set.Keep {
		return found
	}
	if set.State == container.ProgramIntent {
		found = append(found, "an update of the copies of MailArchive did not finish; the next backup finishes it")
	}
	pr, err := openProgramDir(filepath.Join(out, container.ProgramDir), false)
	if err != nil {
		return append(found, "the program folder is not a plain folder ("+err.Error()+")")
	}
	defer pr.close()
	for _, f := range set.Files {
		if f.MissingRuns > 0 {
			continue // already recorded missing
		}
		st := jAbsent
		if pr != nil {
			st, _ = pr.judgeOne(f)
		}
		name := systemName(f.Platform)
		if f.Platform == container.PlatformChecksums {
			name = "the checksum file of " + f.Version
		} else {
			name = "the copy for " + name
		}
		switch st {
		case jAbsent:
			found = append(found, name+" is missing")
		case jChanged:
			found = append(found, name+" was changed — do not run it; the next backup replaces or removes it")
		case jBlocked:
			found = append(found, "something stands where "+name+" belongs")
		case jUnreadable:
			found = append(found, name+" cannot be read")
		case jUnchecked:
			found = append(found, name+" is kept online only and was not checked")
		}
	}
	return found
}
