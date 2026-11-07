package tbengine

import (
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// Sources are what backs up into an archive, as far as this computer can tell: the
// inputs a backup reads, and Auto when one finds its sources itself (-auto: every
// Thunderbird profile it finds). Known is false when nothing names them.
type Sources struct {
	Paths []string
	Auto  bool
	Known bool
}

// ArchiveSources are the archive's sources (design §3.1 step 4 note, RT-10, as settled
// 2026-11-07 [R6-02]): whether one lies at or inside a profile decides W-RECAPTURE and
// the default profile. They are read from what the archive itself records — its last
// run's job (a backup at the command line, by hand or scheduled, records its -input and
// -auto there; an encrypted archive's record keeps none) and the descriptor of a schedule
// installed for it (`mailarchive schedule`) — and from every Desktop job file of this
// computer that backs up into it. Before R6-02 only job files were read, so an archive
// backed up without one never warned. A file that cannot be read is passed over: what it
// would decide is a warning and a default, and backups skip the restored folder anyway
// (§7.2); when nothing names the sources the plan says it cannot tell.
func ArchiveSources(out string) Sources {
	var s Sources
	abs, _ := filepath.Abs(out)
	if dir, err := job.Dir(); err == nil {
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				j, err := job.Read(filepath.Join(dir, e.Name()))
				if err != nil {
					continue
				}
				if jo, _ := filepath.Abs(j.Out); samePath(jo, abs) {
					s.Paths = append(s.Paths, j.Inputs...)
					s.Auto = s.Auto || j.Auto
					s.Known = true
				}
			}
		}
	}
	if lr, st, err := state.ReadLastRun(abs); err == nil && st == state.LastRunPresent && len(lr.Job) > 0 {
		s.add(lr.Job)
	}
	if d, err := schedule.ReadDescriptor(abs); err == nil && len(d.Job) > 0 {
		s.add(d.Job)
		if d.Presync != nil && d.Presync.Profile != "" {
			s.Paths = append(s.Paths, d.Presync.Profile)
		}
	}
	return s
}

// add takes a backup's recorded flags: each -input, and -auto.
func (s *Sources) add(flags []string) {
	s.Known = true
	for i := 0; i < len(flags); i++ {
		f := strings.TrimLeft(flags[i], "-")
		v, hasV := "", false
		if k, val, ok := strings.Cut(f, "="); ok {
			f, v, hasV = k, val, true
		}
		switch f {
		case "auto":
			s.Auto = s.Auto || !hasV || v == "true"
		case "input":
			if !hasV && i+1 < len(flags) {
				i++
				v = flags[i]
			}
			if v != "" {
				s.Paths = append(s.Paths, v)
			}
		}
	}
}

// ArchiveInputs are ArchiveSources' paths (kept for the callers that need only those).
func ArchiveInputs(out string) []string { return ArchiveSources(out).Paths }

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	return ea == nil && eb == nil && ra == rb
}
