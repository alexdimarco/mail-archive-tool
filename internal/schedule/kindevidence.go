package schedule

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/job"
)

// KindEvidence is the set of kinds owner-side state attests for an archive,
// computed from the run-job job file, the desktop configuration and the managed
// cron block — NEVER from a descriptor's Job, which an archive writer can forge
// (§4.6.5b, §5.2). The program builds it once and passes it to Install and to
// health.Gather, so a forged Job can neither add nor suppress a refusal.
type KindEvidence struct {
	// Corroborated is the set of kinds owner-side state attests.
	Corroborated map[Kind]bool
	// JobPath is the corroborated run-job's job file (its basename yields the
	// entry name); "" when no run-job is corroborated.
	JobPath string
}

// Has reports whether owner-side state corroborates kind k.
func (e KindEvidence) Has(k Kind) bool { return e.Corroborated[k] }

// Any reports whether owner-side state corroborates any kind.
func (e KindEvidence) Any() bool { return len(e.Corroborated) > 0 }

// BuildKindEvidence corroborates kinds for the archive out, reading only
// owner-side state: the run-job job file (through internal/job's secure read),
// the desktop configuration (captureOK, supplied by the program so
// internal/schedule never imports internal/desktop), and the managed cron block
// on cron hosts. A descriptor's Job never enters (§4.6.5b).
func BuildKindEvidence(out string, captureOK func(out string) bool) KindEvidence {
	ev := KindEvidence{Corroborated: map[Kind]bool{}}
	if p, ok := corroboratedRunJob(out); ok {
		ev.Corroborated[KindRunJob] = true
		ev.JobPath = p
	}
	if captureOK != nil && captureOK(out) {
		ev.Corroborated[KindCapture] = true
	}
	if k := cronBlockKindFor(out); k != "" {
		ev.Corroborated[k] = true
	}
	return ev
}

// corroboratedRunJob finds a run-job whose job file this program can securely
// read and whose Out is the same archive as out: the default-named file first,
// then any secure job file in job.Dir() whose Out matches under SameArchivePath.
// The descriptor's Job path is never followed (§5.2). The matching file's path is
// the corroborated job path.
func corroboratedRunJob(out string) (string, bool) {
	if p, err := job.PathFor(DefaultNameFor(out)); err == nil {
		if j, err := job.Read(p); err == nil && SameArchivePath(j.Out, out, runtime.GOOS) {
			return p, true
		}
	}
	dir, err := job.Dir()
	if err != nil {
		return "", false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if j, err := job.Read(p); err == nil && SameArchivePath(j.Out, out, runtime.GOOS) {
			return p, true
		}
	}
	return "", false
}

// cronBlockKindFor returns the kind of the managed cron block for out's schedule
// name, or "" off cron hosts or when no matching block names out. The block's
// command line is read back today (G8-AC6), so on cron hosts it stands for the
// recorded kind even when the archive's descriptors are gone.
func cronBlockKindFor(out string) Kind {
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
	default:
		return ""
	}
	tab := readCrontab()
	marker := CronMarker(DefaultNameFor(out))
	if !SameArchivePath(cronBlockOut(tab, marker), out, runtime.GOOS) {
		return ""
	}
	lines := strings.Split(tab, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == marker && i+1 < len(lines) {
			if argv := cronLineArgv(lines[i+1]); len(argv) > 1 {
				return KindOf(argv[1:]) // argv[0] is the program; the job arguments follow
			}
		}
	}
	return ""
}
