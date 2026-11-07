package tbengine

import (
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/job"
)

// ArchiveInputs are the inputs of every scheduled job of this computer that backs up
// into the archive at out (design §3.1 step 4 note, RT-10: "no input of any job for
// this archive lies inside it"): whether one lies in a profile decides W-RECAPTURE and
// the default profile. A job file that cannot be read is passed over: what it would
// decide is a warning and a default, and backups skip the restored folder anyway (§7.2).
func ArchiveInputs(out string) []string {
	dir, err := job.Dir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	abs, _ := filepath.Abs(out)
	var in []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		j, err := job.Read(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		jo, _ := filepath.Abs(j.Out)
		if samePath(jo, abs) {
			in = append(in, j.Inputs...)
		}
	}
	return in
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	return ea == nil && eb == nil && ra == rb
}
