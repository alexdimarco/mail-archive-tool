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

// PlanCopyExport sizes and names a resolved selection and binds it to its source,
// destination and options. It is a dry run: it writes nothing to the destination,
// the archive or the state directory (P5, MA-412), and only reads free space, the
// destination's filesystem type and its inherited access control.
func (e *CopyExportEnv) PlanCopyExport(in CopyPlanInput) (*CopyPlan, error) {
	if len(in.Messages) == 0 {
		return nil, &CopyRefusal{ID: "R-h", Message: "Select at least one folder or message to preview."}
	}
	if len(in.Messages) > copyMaxMessages {
		return nil, &CopyRefusal{ID: "R-i", Message: fmt.Sprintf("This selection has %s messages — one export copies up to 200,000. Split it into several exports, for example by folder.", commas(len(in.Messages)))}
	}
	if in.Options.ReadableHTML || in.Options.Attachments {
		return nil, &CopyRefusal{ID: "R-h", Message: "Readable HTML and extracted attachments aren't available in this version of MailArchive — turn them off and preview again."}
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
	outside, err := e.CheckDestination(dest, in.Archives)
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
	var reserve, record int64
	for i := range p.Messages {
		m := &p.Messages[i]
		if m.SizeHint >= 0 {
			p.EstimateBytes += m.SizeHint
		} else {
			p.UnknownSize++
		}
		pl := namer.place(m, 1)
		rel := pl.Path()
		if len(p.SamplePaths) < 10 {
			p.SamplePaths = append(p.SamplePaths, rel)
		}
		n := utf16Len(filepath.ToSlash(rootAbs) + "/" + rel)
		if n > p.LongestPath {
			p.LongestPath = n
		}
		if n > copyPathWarn {
			p.Over259++
		}
		// The manifest entry's bound: its JSON-escaped strings (a byte can escape to
		// six) plus the fixed fields, and one SUMS line per file (RR2-C5).
		strs := len(m.SourceID) + len(m.InternetMessageID) + len(m.Subject) + len(m.From) + len(m.FromName) + len(m.FolderPath()) + len(rel)
		reserve += int64(400+12*strs) + int64(64+2+len(rel)+1)
		record += int64(copyRecordLineBound) + int64(300+6*strs)
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
