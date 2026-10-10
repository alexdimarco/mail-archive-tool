package schedule

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

// An archive's descriptors are untrusted input (a folder is written by whoever
// handed it over), and the entry name one records decides which scheduler entry
// an operation deletes or names. So a recorded name drives an operation on "the
// archive's entry" only when it binds to THIS archive (G8-AO1, AI1, AI2):
//
//   - the archive's own derived names (DefaultNameFor, and that plus -verify)
//     always bind; a name of the derived shape that is another archive's does not;
//   - a recorded job that backs up a folder that still exists elsewhere on this
//     host is a copy of that archive's record, not this one's; one for a folder
//     that is gone is this archive's own record from before it moved (K1);
//   - a custom name is then checked against what the host can say whose it is
//     without reading the entry back (UJ-6 holds for launchd and Task Scheduler):
//     on cron the managed block's -out must be this archive (or the folder it
//     was moved from), and on Windows the wrapper is ours, so a name with none
//     (a root-folder task of another program) is no entry of this tool's.
//
// A custom name on launchd stays unbound beyond the job's -out: telling another
// archive's custom entry from this one's would read the plist back (UJ-6).

// derivedNameRe is the shape DefaultNameFor derives, with or without -verify.
var derivedNameRe = regexp.MustCompile(`^mailarchive-[0-9a-f]{8}(-verify)?$`)

// RecordedBackupName returns the schedule name the archive's backup descriptor
// records, for an operation that acts on that entry (`-out X -remove`, the GUI's
// remove), or the reason it cannot be trusted as this archive's.
func RecordedBackupName(out string) (string, error) { return recordedBackupNameOn(out, runtime.GOOS) }

func recordedBackupNameOn(out, goos string) (string, error) {
	d, err := ReadDescriptor(out)
	if err != nil {
		return "", err
	}
	if err := bindName(out, d, filepath.Join(out, DescriptorName), goos, true); err != nil {
		return "", err
	}
	return d.Name, nil
}

// RecordedVerifyName returns the entry name the archive's verify records for an
// operation that acts on it (the removal hint, the superseded-verify note): the
// verify descriptor's name, or an older release's backup descriptor that records
// the verify itself, when it binds to this archive.
func RecordedVerifyName(out string) (string, bool) { return recordedVerifyNameOn(out, runtime.GOOS) }

func recordedVerifyNameOn(out, goos string) (string, bool) {
	if d, err := ReadVerifyDescriptor(out); err == nil {
		if bindName(out, d, filepath.Join(out, VerifyDescriptorName), goos, true) != nil {
			return "", false
		}
		return d.Name, true
	}
	if d, err := ReadDescriptor(out); err == nil && descriptorIsVerify(d) {
		if bindName(out, d, filepath.Join(out, DescriptorName), goos, true) != nil {
			return "", false
		}
		return d.Name, true
	}
	return "", false
}

// nameBoundOn reports whether the entry name descriptor d records is this
// archive's, on the scheduler goos selects.
func nameBoundOn(out string, d Descriptor, goos string) error {
	return bindName(out, d, "the descriptor", goos, true)
}

// bindDescriptorName is the part of the binding that needs no scheduler state,
// for the reader of the verify descriptor.
func bindDescriptorName(out string, d Descriptor, path string) error {
	return bindName(out, d, path, "", false)
}

// bindName implements the rules above; withHost adds the scheduler-state checks
// for goos. path names the file in the error.
func bindName(out string, d Descriptor, path, goos string, withHost bool) error {
	def := DefaultNameFor(out)
	if d.Name == def || d.Name == def+"-verify" {
		return nil
	}
	jo := jobOut(d.Job)
	host, _ := os.Hostname()
	sameHost := d.Host == "" || host == "" || d.Host == host
	from := "" // the folder this archive was moved from, when it was
	if sameHost {
		switch {
		case jo == "":
			return fmt.Errorf("%s records the schedule %q with no -out, so nothing says it is this archive's entry: pass -name NAME to name the entry yourself", path, d.Name)
		case sameDir(jo, out):
		default:
			if _, err := os.Stat(jo); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s records the schedule %q for %s, not for this archive (a copied descriptor): pass -name NAME to name the entry yourself, or delete the file", path, d.Name, jo)
			}
			from = jo // gone: this archive, moved
		}
	} else {
		if !withHost {
			// A record from another host names its entries as that host derived them
			// and mounts the folder; this host cannot judge it, and `status` reports
			// it as another host's. Only an operation that acts here refuses it.
			return nil
		}
		return fmt.Errorf("%s records the schedule %q on host %q, not this one: pass -name NAME to name an entry of this host", path, d.Name, d.Host)
	}
	if derivedNameRe.MatchString(d.Name) && !(from != "" && (d.Name == DefaultNameFor(from) || d.Name == DefaultNameFor(from)+"-verify")) {
		return fmt.Errorf("%s records the schedule %q, the derived name of another archive's entry, not this archive's: pass -name NAME to name the entry yourself, or delete the file", path, d.Name)
	}
	if !withHost {
		return nil
	}
	switch goos {
	case "windows":
		if _, err := os.Stat(DefaultWrapperPath(d.Name)); err != nil {
			return fmt.Errorf("%s records the task %q, but this tool has no wrapper for it (%s): it is not an entry this tool installed — pass -name NAME to remove it by name anyway", path, d.Name, DefaultWrapperPath(d.Name))
		}
	case "darwin":
	default:
		if other := cronBlockOut(readCrontab(), CronMarker(d.Name)); other != "" && !sameDir(other, out) && (from == "" || !sameDir(other, from)) {
			return fmt.Errorf("%s records the schedule %q, but that entry backs up %s, not this archive: pass -name NAME to name the entry yourself", path, d.Name, other)
		}
	}
	return nil
}
