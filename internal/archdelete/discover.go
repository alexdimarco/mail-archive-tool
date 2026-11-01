package archdelete

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// Discoverer lists a folder's scheduler entries for the plan (§5.1 [C6]). It only
// reads: nothing is stopped or changed.
type Discoverer interface {
	Discover(folder string) (Discovery, error)
}

// Entry is one scheduler entry Delete stops (§5.3 step 2), and how it was found.
type Entry struct {
	Name string
	// From is "derived" (DefaultNameFor and its -verify), "descriptor", "verify
	// descriptor" or "job file".
	From string
	// Out is the -out spelling the entry records ("" for a derived name).
	Out string
	// JobFile is the job file that records it, for an entry found from one.
	JobFile string
	// RegisteredFor is the other folder whose name the entry is registered under:
	// the current archive's capture registered under captureBackupFor() (§5.1 (c)).
	// That folder is not one of this archive's spellings.
	RegisteredFor string
}

// Note is a descriptor the plan lists without acting on it: one whose name
// MailArchive cannot confirm is this archive's [R3:NEW-FINDING-B], or one another
// computer wrote [NF-OTHER-HOST-DESCRIPTOR]. The file is deleted with the folder.
type Note struct {
	File string
	Name string
	Host string
	Text string
}

// Discovery is what discovery found.
type Discovery struct {
	Entries     []Entry
	Unconfirmed []Note
	OtherHost   []Note
	// CheckYourself are entries listed as "not removed: check it yourself" (macOS
	// custom names no descriptor or job file binds, UJ-6).
	CheckYourself []string
	// Spellings are the -out spellings the entries record, the folder's own first.
	Spellings []string
}

// Names are the entry names, in order.
func (d Discovery) Names() []string {
	out := make([]string, 0, len(d.Entries))
	for _, e := range d.Entries {
		out = append(out, e.Name)
	}
	return out
}

// notRemoved are the schedules the plan lists but Delete does not stop, as the
// saved plan names them (§8.1 "not removed: check it yourself"): those a descriptor
// records that MailArchive cannot confirm are this archive's, another computer's
// (with that computer's name), and the macOS custom names.
func (d Discovery) notRemoved() []string {
	var out []string
	for _, n := range d.Unconfirmed {
		out = append(out, n.Name)
	}
	for _, n := range d.OtherHost {
		out = append(out, fmt.Sprintf("%s (on computer %s: remove it there)", n.Name, n.Host))
	}
	return append(out, d.CheckYourself...)
}

// logNames are the names whose <name>.log and <name>.stderr.log a run of this
// folder writes into it: this computer's entries, another computer's (§4.1), and
// those a descriptor in the folder records that MailArchive cannot confirm — the
// descriptor is MailArchive's file and is removed with the folder, and so are the
// logs of the name it records (del-s1a-A3-06).
func (d Discovery) logNames() []string {
	out := d.Names()
	for _, n := range d.OtherHost {
		out = append(out, n.Name)
	}
	for _, n := range d.Unconfirmed {
		out = append(out, n.Name)
	}
	return out
}

// FileDiscovery is the part of §5.1 that does not ask the scheduler for its
// entries: the derived names, the folder's descriptors (read before anything is
// removed) and every job file in job.Dir() whose Out is the same folder under
// schedule.SameArchivePath — every match, not the first. It reads files; and, as
// schedule.RecordedBackupName does for a custom-named entry, on Linux it reads the
// crontab (crontab -l, read-only) and on Windows the entry's side-files, to confirm
// a descriptor's name (conformance note). Asking the scheduler for its entries
// (OurInstalled, with a time limit) is the caller's: the Desktop's and the command
// line's.
//
// A descriptor counts as this archive's only when the -out it records is this
// folder (schedule.SameArchivePath: by identity, or by spelling when that cannot be
// read). schedule's own binding also accepts a recorded -out that no longer exists
// as "this archive, moved from there"; an unplugged drive looks the same, and a
// copy of an archive whose drive is unplugged carries that archive's descriptor, so
// for a delete such a descriptor is listed, never acted on (del-s1a-A1-02). A
// descriptor that records this folder under another spelling (another case, an
// 8.3 name, a mapped drive) with the name derived from that spelling is this
// archive's (del-s1a-A3-03).
type FileDiscovery struct {
	// GOOS selects SameArchivePath's rule; "" is runtime.GOOS.
	GOOS string
}

// descriptorCap bounds a descriptor read: a few hundred bytes of owner JSON.
const descriptorCap = 64 * 1024

// Discover implements Discoverer.
func (fd FileDiscovery) Discover(folder string) (Discovery, error) {
	goos := fd.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	var d Discovery
	seen := map[string]bool{}
	addEntry := func(e Entry) {
		if seen[e.Name] {
			return
		}
		seen[e.Name] = true
		d.Entries = append(d.Entries, e)
	}
	def := schedule.DefaultNameFor(folder)
	addEntry(Entry{Name: def, From: "derived"})
	addEntry(Entry{Name: def + "-verify", From: "derived"})

	host, _ := os.Hostname()
	for _, desc := range []struct {
		file, what string
		bound      func() (string, bool)
	}{
		{schedule.DescriptorName, "descriptor", func() (string, bool) {
			n, err := schedule.RecordedBackupName(folder)
			return n, err == nil
		}},
		{schedule.VerifyDescriptorName, "verify descriptor", func() (string, bool) { return schedule.RecordedVerifyName(folder) }},
	} {
		rd, present, err := readDescriptorIn(folder, desc.file)
		if err != nil {
			return Discovery{}, err
		}
		if !present {
			continue
		}
		path := filepath.Join(folder, desc.file)
		if rd.Host != "" && host != "" && rd.Host != host {
			d.OtherHost = append(d.OtherHost, Note{File: path, Name: rd.Name, Host: rd.Host,
				Text: fmt.Sprintf("Computer %s also backs up into this folder; remove its schedule there", rd.Host)})
			continue
		}
		out := jobOutOf(rd.Job)
		if confirmed(folder, out, rd.Name, goos, desc.bound) {
			addEntry(Entry{Name: rd.Name, From: desc.what, Out: out})
			continue
		}
		d.Unconfirmed = append(d.Unconfirmed, Note{File: path, Name: rd.Name,
			Text: fmt.Sprintf("%s names the schedule %s, which MailArchive cannot confirm is this archive's: not removed, check it yourself", path, rd.Name)})
	}

	if dir, err := job.Dir(); err == nil {
		if ents, err := os.ReadDir(dir); err == nil {
			for _, e := range ents {
				if e.IsDir() || !schedule.IsJobFileName(e.Name()) {
					continue
				}
				p := filepath.Join(dir, e.Name())
				j, err := job.Read(p)
				if err != nil || j.Version < 1 || !schedule.SameArchivePath(j.Out, folder, goos) {
					continue
				}
				addEntry(Entry{Name: strings.TrimSuffix(e.Name(), ".json"), From: "job file", Out: j.Out, JobFile: p})
			}
		}
	}

	spell := map[string]bool{}
	for _, s := range append([]string{folder}, outsOf(d.Entries)...) {
		if s != "" && !spell[s] {
			spell[s] = true
			d.Spellings = append(d.Spellings, s)
		}
	}
	return d, nil
}

// confirmed reports whether a same-computer descriptor recording the schedule name
// with the -out out is this archive's: out must be this folder (never a folder
// that is gone: it may be another archive's unplugged drive), and the name either
// derived from out or from folder (the same folder, spelled two ways), or bound
// to it by schedule's own rules (bound: a custom name, with its Windows side-file
// and Linux cron checks).
func confirmed(folder, out, name, goos string, bound func() (string, bool)) bool {
	if out == "" || !schedule.SameArchivePath(out, folder, goos) {
		return false
	}
	for _, s := range []string{out, folder} {
		if def := schedule.DefaultNameFor(s); name == def || name == def+"-verify" {
			return true
		}
	}
	b, ok := bound()
	return ok && b == name
}

func outsOf(es []Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Out)
	}
	return out
}

// readDescriptorIn reads one of the folder's descriptors through an os.Root on it
// (util.OpenRegularFileIn), as untrusted input. present is false when there is
// none. A descriptor that is there but cannot be read — not a regular file, not
// JSON, or naming an invalid schedule (ESC-10) — is §7's descriptor refusal,
// worded with schedule.UnreadableDescriptorRemedy's sense [R3:NEW-FINDING-B].
func readDescriptorIn(folder, name string) (schedule.Descriptor, bool, error) {
	var d schedule.Descriptor
	root, err := os.OpenRoot(folder)
	if err != nil {
		return d, false, refuse(CaseUnreachable, msgUnreachable)
	}
	defer root.Close()
	fi, err := root.Lstat(name)
	if isNotExist(err) {
		return d, false, nil
	}
	path := filepath.Join(folder, name)
	if err == nil {
		if link, lerr := util.IsLinkLike(path); fi.Mode()&os.ModeSymlink != 0 || lerr != nil || link {
			return d, true, linkRefusal(path)
		}
	}
	unreadable := refuse(CaseDescriptor, "This archive's schedule record %s cannot be read, so MailArchive cannot tell which schedule it records. If no schedule for this archive is installed, delete %s, then click Delete again; otherwise reinstall that schedule to rewrite it.", path, path)
	f, err := util.OpenRegularFileIn(root, name)
	if err != nil {
		return d, true, unreadable
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, descriptorCap+1))
	if err != nil || len(data) > descriptorCap || json.Unmarshal(data, &d) != nil {
		return d, true, unreadable
	}
	d.Name, d.Host = util.StripControl(d.Name), util.StripControl(d.Host)
	for i := range d.Job {
		d.Job[i] = util.StripControl(d.Job[i])
	}
	if _, err := schedule.SanitizeName(d.Name); err != nil {
		return d, true, unreadable
	}
	return d, true, nil
}

// jobOutOf is the -out a recorded job carries (`-out V` or `-out=V`), or "".
func jobOutOf(args []string) string {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		flag := strings.TrimLeft(a, "-")
		if flag == "out" && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(flag, "out="); ok {
			return v
		}
	}
	return ""
}
