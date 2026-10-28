package schedule

import (
	"errors"
	"fmt"
	"io/fs"
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

// CaptureCorroborator reports whether the desktop configuration corroborates a
// capture for out. The program sets it once at startup (internal/desktop supplies
// it, so internal/schedule never imports internal/desktop); nil means no capture
// is corroborated — e.g. a command-line-only process that never set it. This is
// how schedule "receives the capture predicate from desktop" (§4.6.5b) without an
// import, and it is trusted program code, never the archive.
var CaptureCorroborator func(out string) bool

// BuildKindEvidence corroborates kinds for the archive out, reading only
// owner-side state: the run-job job file (through internal/job's secure read),
// the desktop configuration (via CaptureCorroborator), and the managed cron block
// on cron hosts. A descriptor's Job never enters (§4.6.5b).
func BuildKindEvidence(out string) KindEvidence {
	ev := KindEvidence{Corroborated: map[Kind]bool{}}
	if p, ok := corroboratedRunJob(out); ok {
		ev.Corroborated[KindRunJob] = true
		ev.JobPath = p
	}
	if CaptureCorroborator != nil && CaptureCorroborator(out) {
		ev.Corroborated[KindCapture] = true
	}
	if k := cronBlockKindFor(out); k != "" {
		ev.Corroborated[k] = true
	}
	return ev
}

// CheckKind is the §4.6.5c cross-kind guard: it reports the refusal (or nil) for
// installing kind `install` under archive `out`'s schedule named `name`, given
// owner-side evidence ev and the kind `recorded` of the descriptor already at out
// (KindNone when there is none). Neither trigger lets a forged descriptor Job
// suppress a refusal:
//
//	(1) any kind owner-side state corroborates differs from `install` — a real
//	    schedule of another kind is there, so this install would replace it. This
//	    reads only ev (the owner's config dir and crontab), never the descriptor's
//	    Job, so forging Job cannot suppress it.
//	(2) `recorded` is export and nothing is corroborated — an off-cron export has
//	    no owner-side record, so its descriptor stands for it. This reads the
//	    recorded export only to ADD a refusal, never to suppress one.
//
// A verify is never guarded (it coexists under its own name). A same-kind
// re-install passes, so R14 idempotence holds. CheckKind is exported so the
// wizard's offerSchedule and the SE3 design's install call it before writing, and
// installOn calls it too (§4.6.5c). The remover wording is refined per surface by
// §4.6.3's RepairRoute.
func CheckKind(ev KindEvidence, install, recorded Kind, out, name string) error {
	if install == KindVerify {
		return nil
	}
	for _, ck := range []Kind{KindRunJob, KindCapture, KindExport} { // stable order for a deterministic message
		if ev.Corroborated[ck] && ck != install {
			return fmt.Errorf("archive %s already has a %s schedule named %q: an archive has one schedule, and this install would replace it. Remove that schedule first, or archive into a separate folder.", out, ck, name)
		}
	}
	if recorded == KindExport && install != KindExport && !ev.Any() {
		return fmt.Errorf("archive %s already has an export schedule named %q with no owner-side record to compare: this install would replace it. Remove it first (mailarchive schedule -out %s -remove), or archive into a separate folder.", out, name, out)
	}
	return nil
}

// recordedKind is the kind of the backup schedule descriptor already recorded for
// archive out: KindNone when there is none, KindUnknown when one is present but
// unreadable. Read only to ADD a refusal (CheckKind trigger 2), never to suppress
// one (§4.6.5c).
func recordedKind(out string) Kind {
	if d, err := ReadDescriptor(out); err == nil {
		return KindOf(d.Job)
	}
	if _, err := os.Stat(filepath.Join(out, DescriptorName)); err != nil {
		return KindNone
	}
	return KindUnknown
}

// corroboratedRunJob finds a run-job whose job file this program can securely
// read and whose Out is the same archive as out: the default-named file first,
// then any real job file in job.Dir() whose Out matches under SameArchivePath.
// Only a real job file counts (readRunJobFile, MA-874): the Desktop's own settings
// file and MailArchive's other files in that folder are never taken for one. The
// descriptor's Job path is never followed (§5.2). The matching file's path is the
// corroborated job path.
func corroboratedRunJob(out string) (string, bool) {
	if p, err := job.PathFor(DefaultNameFor(out)); err == nil {
		if j, err := readRunJobFile(p); err == nil && SameArchivePath(j.Out, out, runtime.GOOS) {
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
		if e.IsDir() || !IsJobFileName(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if j, err := readRunJobFile(p); err == nil && SameArchivePath(j.Out, out, runtime.GOOS) {
			return p, true
		}
	}
	return "", false
}

// notJobFiles are the files MailArchive keeps in the job folder (job.Dir) beside the job files
// that are not job files, however they parse: the Desktop's settings (whose "out" key reads as a
// job's Out), the Microsoft 365 configuration, the list of known archives and the last start
// failure; notJobFilePrefixes begin the running Desktop's and a serve's instance records, which
// carry an "out" too, and the Microsoft 365 sign-in caches (graph-token-<tenant>-<client>[-<mailbox>]
// .json, from graphconfig.DefaultTokenCachePath and the command line's own), which a schedule name
// could otherwise match (MA-874). Kept in step with their writers by
// TestTheJobFolderOwnFilesAreNeverJobFiles.
var notJobFiles = map[string]bool{
	"desktop-settings.json": true,
	"graph-config.json":     true,
	"known-archives.json":   true,
	"last-failure.json":     true,
}

var notJobFilePrefixes = []string{"desktop-instance", "serve-", "graph-token-"}

// IsJobFileName reports whether base is named as a scheduled backup's job file is named:
// <schedule name>.json, where the name passes SanitizeName (DefaultNameFor's
// "mailarchive-<hash>" is one) and does not begin with a dot, and is never one of MailArchive's
// own other files in the job folder (notJobFiles, notJobFilePrefixes). It looks at the name
// only (MA-874).
func IsJobFileName(base string) bool {
	lower := strings.ToLower(base)
	if notJobFiles[lower] || !strings.HasSuffix(base, ".json") {
		return false
	}
	for _, p := range notJobFilePrefixes {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}
	name := strings.TrimSuffix(base, ".json")
	if strings.HasPrefix(name, ".") {
		return false
	}
	_, err := SanitizeName(name)
	return err == nil
}

// readRunJobFile reads p as a scheduled backup's job file, and only as one (MA-874): named as job
// files are named (IsJobFileName), read securely by job.Read (out present, a version this
// program reads), and carrying the version every job.Write has recorded since the format began.
func readRunJobFile(p string) (job.Job, error) {
	if !IsJobFileName(filepath.Base(p)) {
		return job.Job{}, fmt.Errorf("%s is not a scheduled backup's job file", p)
	}
	j, err := job.Read(p)
	if err != nil {
		return job.Job{}, err
	}
	if j.Version < 1 {
		return job.Job{}, fmt.Errorf("job file %s records no version: it was not written as a job file", p)
	}
	return j, nil
}

// RemoveJobFile deletes the scheduled backup's job file at path, and nothing else (MA-874): a
// path outside the job folder, or one not named as job files are named (IsJobFileName: never
// the Desktop's settings file or MailArchive's other files there), is refused with nothing
// deleted. A missing file is not an error. Every remover of a job file goes through it.
func RemoveJobFile(path string) error {
	dir, err := job.Dir()
	if err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(dir) || !IsJobFileName(filepath.Base(path)) {
		return fmt.Errorf("not removing %s: it is not a scheduled backup's job file", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
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
