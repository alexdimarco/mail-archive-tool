package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/util"
)

// The copy-export engine (docs/design-extraction-workspace.md §4.7-§4.19, §5,
// slice E2): the plan a preview resolves, the run that writes one new export
// directory from it, and the local run record that is the app's truth about the
// run. It is a library: the dashboard's pages and endpoints are slice E3's, the
// archive source E4's, Readable HTML E5's and extracted attachments E6's, and
// where the engine must hand one of them a function (the retry reads, the launch
// sweep, "Finish as partial") it is here and never an HTTP route.
//
// Files: copyexport.go (types, refusals, the environment), copyexport_names.go
// (the namer), copyexport_fs.go and its per-OS placement (the contained writer),
// copyexport_record.go (the run record, journal and fingerprint ledger),
// copyexport_run.go (Start and the run loop), copyexport_classify.go (the failure
// classes and the awake-time breaker), copyexport_final.go (README, manifest,
// sums, the seal-and-finish step, Finish as partial and Check and finish),
// copyexport_reconcile.go (the launch sweep and the identity checks),
// copyexport_posture.go (the hand-over posture) and copyexport_graph.go (the
// Microsoft 365 adapter).

// CopyOutcome is the engine's exit partition, extract's (X1): complete is 0, a
// run with any problem or a cancel is 3, a run-fatal stop or a refusal is 1.
type CopyOutcome int

const (
	CopyOutcomeComplete CopyOutcome = 0
	CopyOutcomePartial  CopyOutcome = 3
	CopyOutcomeFailed   CopyOutcome = 1
)

// CopyStatus is a run's final status as the sealed line records it.
type CopyStatus string

const (
	CopyStatusComplete           CopyStatus = "complete"
	CopyStatusPartial            CopyStatus = "partial"
	CopyStatusCancelled          CopyStatus = "cancelled"
	CopyStatusFailed             CopyStatus = "failed"
	CopyStatusFinishedAsPartial  CopyStatus = "finished-as-partial"
	copyStatusManifestFailedName            = "failed" // finished-as-partial's manifest status
)

// Outcome maps a status onto the exit partition.
func (s CopyStatus) Outcome() CopyOutcome {
	switch s {
	case CopyStatusComplete:
		return CopyOutcomeComplete
	case CopyStatusPartial, CopyStatusCancelled:
		return CopyOutcomePartial
	}
	return CopyOutcomeFailed
}

// manifestStatus is the status the export-manifest.json carries for a sealed
// status: finished-as-partial says "failed" there and STOPPED in its marker
// (design §4.13 step 6).
func (s CopyStatus) manifestStatus() string {
	if s == CopyStatusFinishedAsPartial {
		return copyStatusManifestFailedName
	}
	return string(s)
}

// Fixed names and bounds (design §4.9, §4.15).
const (
	copyMarkerName   = "_EXPORT-NOT-FINISHED.txt"
	copyReadmeName   = "README.txt"
	copyManifestName = "export-manifest.json"
	copySumsName     = "SHA256SUMS.txt"
	copyTempPrefix   = ".mailexport-"
	copyTempSuffix   = ".tmp"

	copyMaxMessages  = 200000 // R-i
	copyFilesPerDir  = 1000   // XC12
	copyPathBudget   = 250    // UTF-16 units before the slug shrinks
	copyNameMaxBytes = 255    // one file name's bytes: NAME_MAX on ext4, xfs, btrfs, tmpfs and NFS
	copyPathWarn     = 259    // the length Windows Explorer and some zip tools cannot open
	copyFormatVer    = 1      // the manifest's and the run record's format version

	// copyRecordLineBound is the bound on one progress line of the run record the
	// free-space policy reserves on the state volume: its JSON with one file's
	// path, hash and size and a reason.
	copyRecordLineBound = 1024
)

// CopyMessage is one message of a plan: what the export needs to find it, name its
// files and describe it, taken from the listing (design §4.19: source id, folder
// path, subject, from, received, size hint). Subject and the names are Graph's,
// raw and untrusted: the manifest stores them JSON-escaped and control-stripped,
// never as a path.
type CopyMessage struct {
	SourceID          string          `json:"id"`
	InternetMessageID string          `json:"imid,omitempty"`
	Folder            []CopyFolderRef `json:"folder"` // root to leaf
	Subject           string          `json:"subject"`
	From              string          `json:"from,omitempty"`
	FromName          string          `json:"from_name,omitempty"`
	Received          time.Time       `json:"received"`
	SizeHint          int64           `json:"size"` // bytes, or -1 when unknown
	Mailbox           string          `json:"mailbox,omitempty"`
}

// CopyFolderRef is one level of a message's folder path.
type CopyFolderRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FolderPath is the folder's display path for the manifest ("Inbox/Clients").
func (m *CopyMessage) FolderPath() string {
	names := make([]string, len(m.Folder))
	for i, f := range m.Folder {
		names[i] = util.StripControl(f.Name)
	}
	return strings.Join(names, "/")
}

// CopyOptions are a run's options.
type CopyOptions struct {
	PreserveFolders bool `json:"preserve_folder_structure"`
	ReadableHTML    bool `json:"readable_html"`       // slice E5: the engine refuses it until then
	Attachments     bool `json:"extract_attachments"` // slice E6: the same
}

// CopySourceInfo describes the source a plan was resolved from.
type CopySourceInfo struct {
	Kind          string `json:"kind"`                     // "m365"; the archive is slice E4's
	Mailbox       string `json:"mailbox,omitempty"`        // the signer
	Tenant        string `json:"tenant,omitempty"`         // the tenant id, when known
	ImmutableIDs  bool   `json:"immutable_ids"`            // the first page's "honored" flag (XC27)
	OriginalMeans string `json:"original_means,omitempty"` // what "original" means for this source (P8)
}

// CopyPlanFolder is one selected folder of the plan: the manifest's selection
// section and the preview's reported-against-found counts.
type CopyPlanFolder struct {
	Folder     string `json:"folder"`
	Subfolders int    `json:"subfolders"`
	Excluded   int    `json:"excluded"`
	Messages   int    `json:"messages"`
	Reported   int    `json:"reported,omitempty"` // Graph's totalItemCount, for the preview's "reports N, M are messages"
}

// CopyChain is the retry set a plan belongs to (design §4.10, §4.16): the earlier
// parts, in the order they were made, and the chain id, the first part's run id.
// A first part has none, and is its own chain.
type CopyChain struct {
	ID      string         `json:"id,omitempty"`
	RetryOf string         `json:"retry_of,omitempty"`
	Parts   []CopyChainRef `json:"parts,omitempty"`
	// RootName is the name this part's export directory takes ("<first
	// root>-part2"); empty for a first part, which takes the usual
	// MailExport-<time>-<hex> name.
	RootName string `json:"root_name,omitempty"`
}

// CopyChainRef names an earlier part of a retry set.
type CopyChainRef struct {
	Part        int    `json:"part"`
	RunID       string `json:"run_id"`
	Root        string `json:"root"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// CopyPlan is the preview: the resolved selection, sized and named, that binds
// the run. A plan writes nothing to build, and the run copies exactly its
// messages (P5, X14). Resolving a selection into messages (the counts, the
// reconciliation, the row handles) is slice E3's: PlanCopyExport takes the
// resolved list.
type CopyPlan struct {
	ID       string
	Created  time.Time
	Expires  time.Time
	Source   CopySourceInfo
	Folders  []CopyPlanFolder
	Messages []CopyMessage
	Options  CopyOptions
	Chain    CopyChain

	Dest         string // the absolute destination directory the export root is created in
	DestIdentity ceIdentity
	// Outside is what the preview learned about the destination's place against the
	// archives (R-f, R-f2); Start compares the destination against it again.
	Outside *CopyOutside

	SelectionHash string // SHA-256 of the ordered message ids

	// What the preview reports (design §4.7 step 3 and 4).
	EstimateBytes int64
	UnknownSize   int
	FreeBytes     uint64
	Reserve       int64 // finalization reserve, sized from the plan (RR2-C5)
	RecordBound   int64 // the run record's bound on the state volume
	ExportName    string
	SamplePaths   []string
	LongestPath   int // UTF-16 units
	Over259       int
	Posture       CopyPosture
	FSType        string
	Warnings      []string
}

// CopyCounts are a run's counts as the manifest and the sealed line carry them.
type CopyCounts struct {
	Selected        int `json:"selected"`
	Exported        int `json:"exported"`
	Failed          int `json:"failed"`
	NotAttempted    int `json:"not_attempted"`
	WithoutOriginal int `json:"without_original"`
	OriginalOnly    int `json:"original_only,omitempty"` // exported without a readable copy (XC35)
}

// CopyStop is why a run stopped before it finished its plan: the refusal id and
// class the page words (design §4.13 table), and the message that was in flight.
// A cancel carries no id.
type CopyStop struct {
	RID       string `json:"rid,omitempty"`
	Class     string `json:"class,omitempty"`
	Message   string `json:"message,omitempty"` // the refusal's wording (§7.6)
	InFlight  string `json:"in_flight,omitempty"`
	StoppedAt string `json:"at,omitempty"`

	ready bool // Message is the finished refusal text
}

// CopyResult is what a run returns.
type CopyResult struct {
	RunID       string
	Root        string // the export directory's absolute path
	RootName    string
	Status      CopyStatus
	Outcome     CopyOutcome
	Counts      CopyCounts
	Fingerprint string
	Stop        *CopyStop
	Label       string
	// Killed names the step a test's fault hook killed the run at; nothing was
	// finalized after it.
	Killed string
}

// CopyRefusal is a refusal before any write, or at Start: nothing was exported
// (design §7.6). Its Message names what, why and the remedy, in X2's voice, and
// never a token, a Graph URL or a stack trace.
type CopyRefusal struct {
	ID      string // "R-g2"
	Message string
	// Root is the empty export folder an R-d refusal left in place, which the page
	// reports (design §4.16); empty for every other refusal.
	Root string
}

func (e *CopyRefusal) Error() string { return e.Message }

// ExitCode is the refusal's exit code on the copy export's partition: 1.
func (e *CopyRefusal) ExitCode() int { return 1 }

// CopySource is what a run reads its messages from. The Microsoft 365 adapter is
// the only one in slice E2 (originals only); the archive adapter is E4's.
type CopySource interface {
	// Describe names the source for the header and the manifest.
	Describe() CopySourceInfo
	// Fetch streams m's original into w, refusing a body larger than maxBytes
	// (zero or less: no cap). Its errors are the source's own: the Microsoft 365
	// adapter returns the Graph client's typed errors, which the engine classifies.
	Fetch(ctx context.Context, m *CopyMessage, w io.Writer, maxBytes int64) (int64, error)
	// Probe is the one control request sent before a no-progress stop (RR2b-C7).
	Probe(ctx context.Context) error
	// RefreshAuth forces one token refresh after a 401 (XC9).
	RefreshAuth(ctx context.Context) error
	// SignedIn is the signer now, read locally (no network): the run polls it
	// every 100 messages (XC7).
	SignedIn() (upn string, ok bool)
}

// CopyExportEnv is the engine's wiring: the local state directory that holds the
// run records, the journal and the fingerprint ledger (design §4.16, §4.19), and
// the seams a test uses. The zero value of every seam is production behaviour.
// One environment serves one process: its ledger mutex serializes every seal.
type CopyExportEnv struct {
	StateDir string // the per-user, local, non-roaming state directory
	Host     string // this host's name, recorded in the journal and the run records
	Version  string // the tool's version, for the manifest

	journalMu   sync.Mutex // covers an append to the journal and its compaction
	ledgerMu    sync.Mutex
	chains      map[string][]string // run record ids by chain id, kept under ledgerMu (§4.16)
	chainsBuilt bool
	sumMu       sync.Mutex // covers sumCache
	sumCache    map[string]ceSumCached

	// seams
	now         func() time.Time
	randHex     func(n int) string
	syncFile    func(*os.File) error
	freeDest    func(dir string) (uint64, error)
	freeState   func(dir string) (uint64, error)
	captureBusy func() bool // true while a capture runs: the export backs off, uncounted (§4.14)
	boot        func() time.Duration
	tick        time.Duration // the breaker's tick (1 s)
	window      time.Duration // the no-progress window (5 minutes of awake time)
	suspendGap  time.Duration // a tick longer than this is a suspend (30 s)
	backoffMin  time.Duration
	backoffMax  time.Duration
	sleep       func(ctx context.Context, d time.Duration) error
	idle        time.Duration // MIMETo's idle window
	overall     time.Duration // MIMETo's overall cap and the per-message awake bound's source
	downloadMax time.Duration // a message's awake-time bound (2 h)
	sizeCap     int64
	posture     func(dir string) CopyPosture
	realized    func(path string, forDir bool) CopyPosture
	faults      *copyFaults
	fsWrap      func(ceFS) ceFS // test wrapper around the contained writer

	// the destination check's seams (design §4.7 step 3)
	view     func(path string) ceView                // classifies how a path is reached
	served   func(server string) bool                // whether a share's server is this computer
	destIDs  func(dest string) ([]ceIdentity, error) // the identities of the destination and its ancestors
	walkDirs int                                     // the archive walk's directory bound
	walkTime time.Duration                           // and its time bound
}

// NewCopyExportEnv is the production environment for stateDir.
func NewCopyExportEnv(stateDir, host, version string) *CopyExportEnv {
	return &CopyExportEnv{StateDir: stateDir, Host: host, Version: version}
}

func (e *CopyExportEnv) nowTime() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

func (e *CopyExportEnv) rand(n int) string {
	if e.randHex != nil {
		return e.randHex(n)
	}
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		panic("copy export: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b)[:n]
}

func (e *CopyExportEnv) fsync(f *os.File) error {
	if e.syncFile != nil {
		return e.syncFile(f)
	}
	return f.Sync()
}

func (e *CopyExportEnv) freeOnDest(dir string) (uint64, error) {
	if e.freeDest != nil {
		return e.freeDest(dir)
	}
	return util.FreeBytes(dir)
}

func (e *CopyExportEnv) freeOnState() (uint64, error) {
	if e.freeState != nil {
		return e.freeState(e.StateDir)
	}
	return util.FreeBytes(e.StateDir)
}

func (e *CopyExportEnv) dur(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

func (e *CopyExportEnv) tickEvery() time.Duration      { return e.dur(e.tick, time.Second) }
func (e *CopyExportEnv) noProgress() time.Duration     { return e.dur(e.window, 5*time.Minute) }
func (e *CopyExportEnv) suspendAbove() time.Duration   { return e.dur(e.suspendGap, 30*time.Second) }
func (e *CopyExportEnv) backoffFirst() time.Duration   { return e.dur(e.backoffMin, time.Second) }
func (e *CopyExportEnv) backoffCeiling() time.Duration { return e.dur(e.backoffMax, time.Minute) }
func (e *CopyExportEnv) idleWindow() time.Duration     { return e.dur(e.idle, 2*time.Minute) }
func (e *CopyExportEnv) overallCap() time.Duration     { return e.dur(e.overall, 2*time.Hour) }
func (e *CopyExportEnv) messageBound() time.Duration   { return e.dur(e.downloadMax, 2*time.Hour) }

func (e *CopyExportEnv) cap() int64 {
	if e.sizeCap > 0 {
		return e.sizeCap
	}
	return copyDefaultSizeCap
}

func (e *CopyExportEnv) pause(ctx context.Context, d time.Duration) error {
	if e.sleep != nil {
		return e.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (e *CopyExportEnv) busy() bool { return e.captureBusy != nil && e.captureBusy() }

// copyDefaultSizeCap is the shared size cap on one $value body: graph.MIMEMaxBytes,
// 256 MiB (ESC-8's safe interim, which the copy export alone carries). It is the
// graph package's own constant, so a later rise of the cap there is the export's too.
const copyDefaultSizeCap = graph.MIMEMaxBytes

// commas formats n with thousands separators, as the refusal texts show counts.
func commas(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + commas(-n)
	}
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// humanBytes is a size as a refusal or a preview shows it: "1.4 GB".
func humanBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
