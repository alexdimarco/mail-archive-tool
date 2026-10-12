package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/util"
)

// copyPlanTTL is how long a preview stays valid before Start refuses it (R-k).
const copyPlanTTL = 30 * time.Minute

// CopyPlanInput is a resolved selection: what the preview endpoint (slice E3)
// hands the engine once it has turned folder entries and picks into messages.
// Resolving a selection (the folder tree, the counts, the reconciliation, row
// handles) is E3's; sizing, naming and binding it is the plan's.
type CopyPlanInput struct {
	Source   CopySourceInfo
	Folders  []CopyPlanFolder
	Messages []CopyMessage
	Options  CopyOptions
	Dest     string // an absolute directory
	Chain    CopyChain
	// Archives are the archive folders the export must stay outside of, by path and
	// by identity (R-f, R-f2; design §4.7 step 3): the configured archive, and in
	// slice E4 another archive folder chosen as the source. Empty: none.
	Archives []string
	// OtherArchive is the member of Archives that is another archive folder, the export's
	// source, read-only [ruling W1]: empty for any other source.
	OtherArchive string
	// ArchiveSkipped is how many records the archive source left out because their files
	// were removed (the redaction belt, §4.6 item 5); the preview reports the count.
	ArchiveSkipped int
}

// copyRootName is the name the export directory takes: MailExport-<local
// YYYY-MM-DD_HHMMSS>-<4 random hex> (XC32: a co-writer cannot pre-create the next
// names). A retry part takes its chain's name.
func (e *CopyExportEnv) copyRootName(chain CopyChain) string {
	if chain.RootName != "" {
		return chain.RootName
	}
	return "MailExport-" + e.nowTime().Format("2006-01-02_150405") + "-" + e.rand(4)
}

// oneFolder reports whether every message of a selection is in one and the same folder.
func oneFolder(ms []CopyMessage) bool {
	if len(ms) == 0 || len(ms[0].Folder) == 0 {
		return false
	}
	id := ms[0].Folder[len(ms[0].Folder)-1].ID
	for _, m := range ms {
		if len(m.Folder) == 0 || m.Folder[len(m.Folder)-1].ID != id {
			return false
		}
	}
	return true
}

// copyTooManyText is R-i: what the selection holds, the cap and what to do. "Split it by folder"
// is no remedy for a selection that is one folder (friction F15), so that case says what
// MailArchive can't do yet and the two things the owner can.
func copyTooManyText(n int, single bool) string {
	if single {
		return fmt.Sprintf("This folder has %s messages — one export copies up to 200,000, and MailArchive can't split one folder across exports in this version. Export part of it by picking its messages by hand (up to 10,000 at a time), or, if it has subfolders, export those one at a time.", commas(n))
	}
	return fmt.Sprintf("This selection has %s messages — one export copies up to 200,000. Split it into several exports, for example by folder.", commas(n))
}

// PlanCopyExport sizes and names a resolved selection and binds it to its source,
// destination and options. It is a dry run: it writes nothing to the destination,
// the archive or the state directory (P5, MA-412), and only reads free space, the
// destination's filesystem type and its inherited access control.
func (e *CopyExportEnv) PlanCopyExport(in CopyPlanInput) (*CopyPlan, error) {
	if len(in.Messages) == 0 {
		return nil, &CopyRefusal{ID: "R-h", Message: "Select at least one folder or message to preview."}
	}
	if len(in.Messages) > copyMaxMessages {
		return nil, &CopyRefusal{ID: "R-i", Message: copyTooManyText(len(in.Messages), oneFolder(in.Messages))}
	}
	dest := in.Dest
	// the same words wherever the destination is checked: empty box, ~ and %NAME% not expanded,
	// a file that is not a folder, a folder that is not there (friction F19)
	if r := checkDestPath(dest); r != nil {
		return nil, r
	}
	droot, err := os.OpenRoot(dest)
	if err != nil {
		return nil, destMissing(dest)
	}
	f, err := droot.Open(".")
	if err != nil {
		droot.Close()
		return nil, destMissing(dest)
	}
	id, _ := readIdentity(f)
	f.Close()
	droot.Close()
	outside, err := e.CheckDestinationOther(dest, in.Archives, in.OtherArchive)
	if err != nil {
		return nil, err
	}

	p := &CopyPlan{Outside: outside,
		ID: e.rand(16), Created: e.nowTime(), Source: in.Source, Folders: in.Folders, Messages: in.Messages,
		Options: in.Options, Chain: in.Chain, Dest: dest, DestIdentity: id, FSType: id.FSType,
	}
	p.Expires = p.Created.Add(copyPlanTTL)
	p.ExportName = e.copyRootName(in.Chain)
	e.sizePlan(p)
	if in.Source.Kind == "archive" {
		p.Archive = &CopyArchiveCounts{Skipped: in.ArchiveSkipped}
		for i := range in.Messages {
			m := &in.Messages[i]
			switch {
			case m.Blocked != "": // the run will list it as failed for good: not counted as having an original or not
				p.Archive.Blocked++
				if p.Archive.BlockedWhy == nil {
					p.Archive.BlockedWhy = map[string]int{}
				}
				p.Archive.BlockedWhy[m.Blocked]++
			case m.NoOrig == "":
				p.Archive.WithOriginal++
				if m.Fix == nil || m.Fix.EML == nil {
					p.Archive.Unattested++
				}
			default:
				p.Archive.WithoutOriginal++
				if m.Quarantine {
					p.Archive.Quarantined++
				}
			}
			if m.Gone {
				p.Archive.Gone++
			}
		}
	}

	h := sha256.New()
	for i := range in.Messages {
		h.Write([]byte(in.Messages[i].SourceID))
		h.Write([]byte{0})
	}
	p.SelectionHash = hex.EncodeToString(h.Sum(nil))

	if free, err := e.freeOnDest(dest); err == nil {
		p.FreeBytes = free
		// The sentence Start would refuse with, raised while the owner can still choose
		// another destination (friction F17).
		if need := p.EstimateBytes + p.Reserve; int64(free) < need {
			p.Warnings = append(p.Warnings, refusalG2(dest, free, need).Message)
		}
	}
	if why := destNotWritable(dest); why != "" {
		p.Warnings = append(p.Warnings, refusalW(dest, why).Message)
	}
	if svc, ok := util.UnderCloudSync(dest); ok {
		p.Warnings = append(p.Warnings, fmt.Sprintf("files you export here will be uploaded to %s", svc))
	}
	p.Warnings = append(p.Warnings, fsTypeWarnings(id.FSType)...)
	if p.UnknownSize > 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%s messages are of unknown size, so the space this export needs is an estimate", commas(p.UnknownSize)))
	}
	if p.OverCap > 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%s messages are larger than this version of MailArchive exports (%d MiB): they will be listed as failed and not copied", commas(p.OverCap), e.cap()>>20))
	}
	p.Posture = e.destinationPosture(dest, id.FSType)
	return p, nil
}

// fsTypeWarnings are the preview's notes for the destination's filesystem type (MA-412): a
// FAT or exFAT drive cannot hold a message over 4 GiB and keeps no permissions. A share is
// not named here; its line is the posture's.
func fsTypeWarnings(fsType string) []string {
	if un, share := unenforceableFS(fsType); un && !share {
		return []string{"this drive is FAT or exFAT: a message over 4 GiB can't be written to FAT32, and file permissions aren't kept"}
	}
	return nil
}

func destMissing(dest string) *CopyRefusal {
	return &CopyRefusal{ID: "R-e", Message: fmt.Sprintf("The folder %s doesn't exist — an export is written into an existing folder. Click Browse to choose one, or Create folder.", dest)}
}

// sizePlan fills the plan's estimate, the reserve and the record bound, and names
// its first target paths and the longest, with the namer in dry-run mode.
func (e *CopyExportEnv) sizePlan(p *CopyPlan) {
	rootAbs := filepath.Join(p.Dest, p.ExportName)
	namer := newCeNamer(p.Options.PreserveFolders, filepath.ToSlash(rootAbs))
	namer.readable = p.Options.ReadableHTML
	namer.attachZip = p.Options.Attachments && p.Options.AttachmentsAsZip
	var reserve, record int64
	for i := range p.Messages {
		m := &p.Messages[i]
		readable := p.Options.ReadableHTML && m.NoOrig == "" // a readable copy beside the original (slice E5)
		switch {
		case m.SizeHint > e.cap():
			p.OverCap++ // listed as larger than this version exports, and no space needed for it
		case m.SizeHint >= 0:
			p.EstimateBytes += m.SizeHint
			if readable {
				p.EstimateBytes += m.SizeHint // the design's factor: a readable copy is about the size of its original
			}
		default:
			p.UnknownSize++
		}
		if hasAttachSource(m, p.Options) && m.SizeHint >= 0 && m.SizeHint <= e.cap() {
			p.EstimateBytes += m.SizeHint * 4 / 5 // the design's factor for extracted attachments: 0.8 of the message (§4.7 step 4)
		}
		pl := namer.place(m, copyFileCountFor(m, p.Options))
		rel := pl.Dir + "/" + pl.Stem + copyFirstSuffix(m)
		if len(p.SamplePaths) < 10 {
			p.SamplePaths = append(p.SamplePaths, rel)
		}
		n := utf16Len(filepath.ToSlash(rootAbs) + "/" + pl.Dir + "/" + pl.Stem + copyLongestSuffixFor(m, p.Options))
		if n > p.LongestPath {
			p.LongestPath = n
		}
		if n > copyPathWarn {
			p.Over259++
		}
		// The manifest entry's bound: its JSON-escaped strings (a byte can escape to
		// six) plus the fixed fields, and one SUMS line per file (RR2-C5).
		strs := len(m.SourceID) + len(m.InternetMessageID) + len(m.Subject) + len(m.From) + len(m.FromName) + len(m.FolderPath()) + len(rel)
		if st := m.State; st != nil { // the listing's state rides in the plan line of the run record (slice E5)
			strs += len(st.Importance) + len(st.Sensitivity)
			for _, c := range st.Categories {
				strs += len(c) + 4
			}
		}
		reserve += int64(400+12*strs) + int64(64+2+len(rel)+1)
		record += int64(copyRecordLineBound) + int64(300+6*strs)
		if readable { // its manifest entry, its sums line, and a progress line that holds a second file and a note
			reserve += int64(200 + 64 + 2 + len(rel) + len(".html") + 1 + 6*(len(noteReadableFailed)+240))
			record += int64(copyRecordLineBound)
		}
		if hasAttachSource(m, p.Options) { // extracted files: a progress line that holds them, which the plan cannot count; each one's manifest entry and sums line join the run's reserve as it is written (CopyRun.derivedReserve)
			record += int64(copyRecordLineBound)
		}
		if copyFileCount(m) == 2 { // the archived page and its zip: a second file, with its manifest entry and sums line
			reserve += int64(200 + 64 + 2 + len(rel) + len(copyLongestSuffix(m)) + 1)
			record += int64(copyRecordLineBound)
		}
		// The archive's own fields ride in the plan line of the run record: the fixity and the notes.
		if m.Rel != "" {
			record += int64(len(m.Rel) + 400)
		}
		for _, note := range m.Notes {
			reserve += int64(6*len(note) + 8)
			record += int64(6*len(note) + 8)
		}
	}
	// README's bound, and one allocation cluster for each finalization file and its
	// temp (README, manifest, sums, the marker's rewrite).
	reserve += 16<<10 + 8*4096
	p.Reserve = reserve
	p.RecordBound = record + 16<<10
}

// jsonSize is the length of v's JSON, for a bound.
func jsonSize(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}
