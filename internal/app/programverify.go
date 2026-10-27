package app

import (
	"path/filepath"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/util"
)

// checkProgramFolder is verify -full's comparison of the program folder and the note
// with what the record seals (program design §6, C18; PG4): read-only — nothing is
// written, swept or renamed — and every finding a warning that never changes the
// verdict on the mail. While an update's intent stands, either side of it is the
// program's own, as the next backup accepts it (R5, R7; the PG4 pass, DUR-2): the note
// before or after it, each copy as it was or as it is coming, and a copy being removed
// already gone. A file in the folder that no entry names is told too (COR-3).
func checkProgramFolder(out string, set *container.ProgramSet, noteDigest string) []string {
	var found []string
	accept := map[string]bool{noteDigest: true}
	intent := set != nil && set.State == container.ProgramIntent
	if intent && set.Note != nil {
		accept[set.Note.Current], accept[set.Note.Incoming] = true, true
	}
	if b, err := util.ReadRegularFile(filepath.Join(out, "README.txt"), maxNoteRead); noteDigest != "" && (err != nil || !accept[NoteDigest(b)]) {
		found = append(found, "the note beside the archive (README.txt) is not the one the last backup wrote — do not follow it; the next backup writes it again")
	}
	if set == nil {
		return found
	}
	if intent {
		found = append(found, "an update of the copies of MailArchive did not finish; the next backup finishes it")
	}
	// Each name the set accounts for, with every version of it that is the program's own.
	type entry struct {
		as       []container.ProgramFile
		removing bool
	}
	names := map[string]*entry{}
	var order []string
	add := func(f container.ProgramFile) {
		if names[f.Name] == nil {
			names[f.Name] = &entry{}
			order = append(order, f.Name)
		}
		names[f.Name].as = append(names[f.Name].as, f)
	}
	for _, f := range set.Files {
		add(f)
	}
	if intent {
		for _, f := range set.Incoming {
			add(f)
		}
		for _, n := range set.Removing {
			if e := names[n]; e != nil {
				e.removing = true
			}
		}
	}
	if len(names) == 0 && (set.Keep == nil || !*set.Keep) {
		return found // none kept, none sealed: there is nothing here to compare
	}
	pr, err := openProgramDir(filepath.Join(out, container.ProgramDir), false)
	if err != nil {
		return append(found, "the program folder is not a plain folder ("+err.Error()+")")
	}
	if pr != nil {
		defer pr.close()
	}
	for _, n := range order {
		e := names[n]
		st := jAbsent
		for _, f := range e.as {
			if f.MissingRuns > 0 && len(e.as) == 1 {
				st = jOK // already recorded missing: status tells it
				break
			}
			if pr == nil {
				break
			}
			if st, _ = pr.judgeOne(f); st == jOK {
				break
			}
		}
		if st == jAbsent && e.removing {
			continue // its removal was under way
		}
		f := e.as[0]
		name := "the copy for " + systemName(f.Platform)
		if f.Platform == container.PlatformChecksums {
			name = "the checksum file of " + f.Version
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
	if pr != nil {
		for _, d := range pr.entries() {
			if n := d.Name(); names[n] == nil && !isProgramTemp(n) && !util.IsLitter(n) {
				found = append(found, "the program folder holds "+n+", which MailArchive did not put there — do not run it")
			}
		}
	}
	return found
}
