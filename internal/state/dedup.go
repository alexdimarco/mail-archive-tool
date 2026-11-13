package state

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// Gmail de-duplication, manifest version 7 (docs/design-gmail-dedup.md §7.1 and §7.4, slice B2).
// B2 declares the schema, the one function that decides the version a manifest is written at,
// load validation and the person's switch; nothing in B2 fills a membership or a merge (slices
// D1, M2 and D2 do). Every field is omitempty, so a manifest with none of them is byte for byte
// what version 6 wrote (MA-329).

// Membership is "this message is also in folder F": a folder other than its record's home
// folder (Record.Folder). Kind is MemberCopy when F's mbox held a byte-identical copy, or
// MemberListed when only F's summary file lists it. FirstSeen is set once (Q-L2).
type Membership struct {
	Folder    string    `json:"folder"`
	Kind      string    `json:"kind"`
	FirstSeen time.Time `json:"first_seen"`
}

// The two kinds of membership.
const (
	MemberCopy   = "copy"
	MemberListed = "listed"
)

// MergedFile is one file of a folded copy that the merge step (slice D2) owns until it deletes
// it. Unit is never written in version 1 (plaintext only, §7.1 fold-check note F1).
type MergedFile struct {
	Path   string `json:"path,omitempty"`
	Unit   string `json:"unit,omitempty"`
	Role   string `json:"role"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Proof  string `json:"proof"`
}

// MergePending is the merge step in progress (slice D2). While it is present the manifest is
// version 8, which a build that reads at most 7 refuses whole (§7.4 fold-check-5 note).
type MergePending struct {
	PlanDigest string       `json:"plan_digest"`
	Started    time.Time    `json:"started"`
	Groups     []MergeGroup `json:"groups"`
	Folded     int          `json:"folded,omitempty"`
	Freed      int64        `json:"freed,omitempty"`
	Damaged    []string     `json:"damaged,omitempty"`
}

// MergeGroup is one survivor and the copies the merge step folds into it.
type MergeGroup struct {
	Survivor        string   `json:"survivor"`
	Copies          []string `json:"copies"`
	Kept            []string `json:"kept,omitempty"`
	SurvivorChanged bool     `json:"survivor_changed,omitempty"`
}

// SummaryStamp is a summary file's size and time at its last whole read (slice M2).
type SummaryStamp struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
}

// DedupGmail is the one value a DedupStores entry holds: the store is switched on to the Gmail
// rule of version 1.
const DedupGmail = "gmail-1"

// The format versions B2 knows. manifestVersion (6) is what a manifest with none of the Gmail
// fields is written at; versionGmail when any is present; versionMergePending while the merge
// step is part-way (§7.4 fold-check-5 note, M1).
const (
	versionGmail        = 7
	versionMergePending = 8
)

// FormatVersion is the version m is written at: 8 while a merge is pending, 7 when it holds any
// Gmail field (a DedupStores or SummaryStamps entry, or a record with Folders, gm_sha256 or
// MergedFiles), otherwise 6. It is the only thing that decides what is stamped (§7.4 fold a;
// F3): save, a fresh manifest, the load-time bump and a fresh sealed record all go through it.
func FormatVersion(m *Manifest) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.formatVersionLocked()
}

// formatVersionLocked is FormatVersion for a caller that holds m.mu (save does; fold check 6's
// S4).
func (m *Manifest) formatVersionLocked() int {
	return FormatVersionOf(View{Entries: m.Entries, DedupStores: m.DedupStores, MergePending: m.MergePending, SummaryStamps: m.SummaryStamps})
}

// FormatVersionOf is FormatVersion over a view's content: the version a sealed record of v is
// written at (a fresh sealed record is FormatVersionOf(View{}), 6).
func FormatVersionOf(v View) int {
	if v.MergePending != nil {
		return versionMergePending
	}
	if len(v.DedupStores) > 0 || len(v.SummaryStamps) > 0 {
		return versionGmail
	}
	for _, r := range v.Entries {
		if len(r.Folders) > 0 || r.GmailSHA256 != "" || len(r.MergedFiles) > 0 {
			return versionGmail
		}
	}
	return manifestVersion
}

// DedupOn reports whether the store token is switched on to the Gmail rule.
func (m *Manifest) DedupOn(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.DedupStores[token] == DedupGmail
}

// SwitchOnGmail switches each store token on to the Gmail rule: the person's yes to Q-B, or a
// new archive's first run. The archive is version 7 from the next save on. It is sticky: nothing
// in version 1 switches a store off (Q-C).
func (m *Manifest) SwitchOnGmail(tokens ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range tokens {
		if !SafeToken(t) {
			continue
		}
		if m.DedupStores == nil {
			m.DedupStores = map[string]string{}
		}
		m.DedupStores[t] = DedupGmail
	}
}

// GmailAsk is what a backup does about the Gmail switch for an archive (§7.4 fold c, Q-B).
type GmailAsk int

const (
	// GmailLeave: nothing is asked or written (no Gmail store left to switch, the person said
	// Not now on this computer, or the run is unattended).
	GmailLeave GmailAsk = iota
	// GmailAskPerson: ask the question once, before or after the run, never blocking it.
	GmailAskPerson
	// GmailSwitchNew: a new archive (no records yet) is switched on by default.
	GmailSwitchNew
)

// GmailDecision is the switch's decision for the Gmail stores gmail (the tokens the run found to
// be Gmail; B1's GmailForStore said yes), given the person's Not now kept on this computer and
// whether a person is there to ask (attended: the Desktop, or a terminal on both stdin and
// stdout). An archive with no records at all is new and is switched on; an existing one is
// asked, never switched, and a scheduled or unattended run never asks and never switches.
func (m *Manifest) GmailDecision(gmail []string, notNow, attended bool) GmailAsk {
	m.mu.Lock()
	defer m.mu.Unlock()
	off := false
	for _, t := range gmail {
		if m.DedupStores[t] != DedupGmail {
			off = true
		}
	}
	switch {
	case !off:
		return GmailLeave
	case len(m.Entries) == 0:
		return GmailSwitchNew
	case notNow || !attended:
		return GmailLeave
	}
	return GmailAskPerson
}

// The switch's words (§7.4 fold c and its F1 note; "1.7" now 1.8, the 2026-11-13 fold). The
// surfaces that show them (the Desktop card, the terminal) are slice B3's.
const (
	GmailQuestion          = "Show Sent Mail and your other Gmail folders in full, and store each email once? Every computer that backs up into this archive then needs MailArchive 1.8 or later."
	GmailQuestionEncrypted = " Emails this archive already holds more than once stay as they are, because it is encrypted."
	GmailSetupLine         = "Gmail: each email is stored once and shown in all of its folders."
)

// GmailSwitchQuestion is Q-B's question for an existing archive; an encrypted one with records
// gains the sentence that its earlier copies stay (the merge step is plaintext only, F1).
func GmailSwitchQuestion(encrypted bool) string {
	if encrypted {
		return GmailQuestion + GmailQuestionEncrypted
	}
	return GmailQuestion
}

// The words of a refusal of a newer format (§7.4 fold f, and its fold-check-5 note for exactly
// version 8). Both are for the owner's sign-off.
const (
	NewerWords  = "This archive needs a newer MailArchive on this computer."
	TidyUpWords = "A newer MailArchive is part-way through tidying this archive (keeping one copy of each Gmail email). Finish it there, or install that version on this computer and choose Resume tidy-up. Until then this computer does not back up into it."
)

// NewerFormatWords is the plain sentence a refusal of a manifest at version v says: the tidy-up
// words for exactly 8, the newer words otherwise.
func NewerFormatWords(v int) string {
	if v == versionMergePending {
		return TidyUpWords
	}
	return NewerWords
}

// validateDedup is load validation of the version-7 fields (MA-1217; §7.1 fold and its
// fold-check notes). A bad membership or gm_sha256 is dropped, never the record. A MergedFile
// whose Path is not a clean relative path is dropped (the merge step's deleter could never have
// reached it); one with a clean Path that fails another check is moved to the record's
// AlsoFiles with every entry of that record sharing its file stem, so the file stays owned and
// is never deleted (all or none). A Microsoft 365 record's Folders are dropped (§8.10). Each
// drop or move is noted in m.DedupNotes for the caller to log. It runs only when no merge is
// pending: a pending merge is refused before this as version 8.
func (m *Manifest) validateDedup() {
	mailbox := map[string]bool{}
	for id, tok := range m.Stores {
		if strings.HasPrefix(id, "mailbox:") {
			mailbox[tok] = true
		}
	}
	keys := make([]string, 0, len(m.Entries))
	for k, r := range m.Entries {
		if len(r.Folders) > 0 || r.GmailSHA256 != "" || len(r.MergedFiles) > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		r := m.Entries[key]
		tok, _, _ := strings.Cut(key, keySeparator)
		if len(r.Folders) > 0 {
			if mailbox[tok] {
				m.note(key, "%d folder membership(s) on a Microsoft 365 record dropped", len(r.Folders))
				r.Folders = nil
			} else {
				r.Folders = m.validMembers(key, r)
			}
		}
		if r.GmailSHA256 != "" && !validDigest(r.GmailSHA256) {
			m.note(key, "a gm_sha256 that is not a sha-256 dropped")
			r.GmailSHA256 = ""
		}
		if len(r.MergedFiles) > 0 {
			r.MergedFiles, r.AlsoFiles = m.validMerged(key, r.MergedFiles, r.AlsoFiles)
		}
		m.Entries[key] = r
	}
}

func (m *Manifest) note(key, format string, a ...any) {
	m.DedupNotes = append(m.DedupNotes, fmt.Sprintf("manifest record %q: ", util.StripControl(key))+fmt.Sprintf(format, a...))
}

// validMembers keeps the memberships of r that name a clean folder other than r's own, of a
// known kind, each folder once.
func (m *Manifest) validMembers(key string, r Record) []Membership {
	var keep []Membership
	seen := map[string]bool{}
	for _, f := range r.Folders {
		switch {
		case !validMemberFolder(f.Folder):
			m.note(key, "a folder membership with a folder that is not a clean relative path dropped")
		case f.Folder == r.Folder:
			m.note(key, "a folder membership naming the record's own folder dropped")
		case f.Kind != MemberCopy && f.Kind != MemberListed:
			m.note(key, "a folder membership of unknown kind %q dropped", util.StripControl(f.Kind))
		case seen[f.Folder]:
			m.note(key, "a second folder membership for one folder dropped")
		default:
			seen[f.Folder] = true
			keep = append(keep, f)
		}
	}
	return keep
}

// validMemberFolder says f is a folder spelled as Record.Folder is: not empty, not absolute, no
// control character, and no empty, "." or ".." segment.
func validMemberFolder(f string) bool {
	if f == "" || util.StripControl(f) != f || strings.HasPrefix(f, "/") || strings.HasPrefix(f, `\`) {
		return false
	}
	for _, s := range strings.Split(strings.ReplaceAll(f, `\`, "/"), "/") {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	return true
}

// Roles of a MergedFile.
const (
	roleEML  = "eml"
	roleHTML = "html"
	roleZip  = "zip"
)

// validMerged is validateDedup's MergedFiles rule; it returns the entries kept and the record's
// AlsoFiles with the moved files added.
func (m *Manifest) validMerged(key string, in []MergedFile, also []string) ([]MergedFile, []string) {
	var clean []MergedFile
	for _, f := range in {
		if _, ok := util.ValidRecordedPath(f.Path); !ok {
			m.note(key, "a merge-step file with a path that is not a clean relative path dropped")
			continue
		}
		clean = append(clean, f)
	}
	bad := map[string]bool{} // file stems with any entry that fails its checks
	for _, f := range clean {
		if !validMergedFile(f) {
			bad[mergedStem(f.Path)] = true
		}
	}
	var keep []MergedFile
	for _, f := range clean {
		if bad[mergedStem(f.Path)] {
			m.note(key, "a merge-step file that fails its checks kept as this email's own file (%s)", util.StripControl(f.Path))
			also = appendOnce(also, f.Path)
			continue
		}
		keep = append(keep, f)
	}
	return keep, also
}

// validMergedFile is a MergedFile's own checks: a known role, a size of 0 or more, a proof that
// is a sha-256, and a sha-256 that is one (an empty one allowed for a page or a zip, S2).
func validMergedFile(f MergedFile) bool {
	if f.Unit != "" || f.Size < 0 || !validDigest(f.Proof) {
		return false
	}
	switch f.Role {
	case roleEML:
		return validDigest(f.SHA256)
	case roleHTML, roleZip:
		return f.SHA256 == "" || validDigest(f.SHA256)
	}
	return false
}

// mergedStem is a message file's name without .html, -attachments.zip or .eml: a copy's page, zip
// and original share it.
func mergedStem(p string) string {
	for _, suf := range []string{"-attachments.zip", ".html", ".eml"} {
		if strings.HasSuffix(p, suf) {
			return strings.TrimSuffix(p, suf)
		}
	}
	return p
}

func appendOnce(list []string, p string) []string {
	for _, s := range list {
		if s == p {
			return list
		}
	}
	return append(list, p)
}
