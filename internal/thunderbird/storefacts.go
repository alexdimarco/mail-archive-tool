package thunderbird

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Limits are an account's own limits on what Thunderbird downloads to this computer
// (completeness design §4.1 step 4 and its TC-9 fold), as Thunderbird applies them: the
// account's own setting, else mail.server.default's, else mailnews.js's built-in default. The
// zero value is Thunderbird's defaults: every age, no size limit, background download on.
type Limits struct {
	MaxAgeDays           int  // autosync_max_age_days: > 0 keeps only that many days (built-in -1)
	SizeLimit            bool // limit_offline_message_size (built-in false)
	MaxSizeKB            int  // max_size, in KB (built-in 50)
	NoBackgroundDownload bool // autosync_offline_stores false (built-in true)
}

// The built-in defaults, mailnews.js (comm-central tip, copy .claude/mozsrc-r5b, and
// .claude/tbsrc): lines 547, 548, 621 and 625.
const (
	defMaxAgeDays    = -1
	defMaxSizeKB     = 50
	limitMaxAge      = "autosync_max_age_days"
	limitSizeOn      = "limit_offline_message_size"
	limitMaxSize     = "max_size"
	limitBackgroundD = "autosync_offline_stores"
)

// StoreFacts is what the completeness check reads of a store's account from the profile's
// prefs.js and user.js, in one read (slice C2; the C1a pass round 2's R2-02 note).
type StoreFacts struct {
	// Account is the account that owns Store (C0c's match, FindAccountForStore's rule); nil when
	// none can be told, and the error says why.
	Account *Account
	// Store is the folder Account owns: the input itself, or, for an input inside a store (one
	// mbox file of it, or a subfolder), the store folder above it (TC-11).
	Store string
	// Whole: the input is the store folder itself, so the walk covers the whole store (TC-11).
	Whole bool
	// Gmail is GmailForStore's answer for Account, from the same read.
	Gmail Gmail
	// Limits are Account's limits, read for an imap account only.
	Limits Limits
	// OtherStores are the folders every other live account's settings name as its own
	// (directory-rel with [ProfD] resolved, and directory): the profile's other stores, whose
	// keys the folder cache's tail mode must never take (R2-02). The matched account's own
	// settings are never in it (ADV-C2-07).
	OtherStores []string
}

// LimitsError says a limit setting of the account has a value of a type Thunderbird never
// writes for it, so what Thunderbird applies cannot be told (the C1b note: a value MailArchive
// cannot read with certainty fails the account match).
type LimitsError struct {
	Setting string
	What    string
}

func (e *LimitsError) Error() string {
	return fmt.Sprintf("MailArchive cannot tell what Thunderbird applies for %s (%s), so it does not tell how much of this account Thunderbird has downloaded", StripControl(e.Setting), e.What)
}

// ReadStoreFacts reads, for the input input of a backup (a store folder, a folder inside one,
// or one mailbox file) in the profile profileDir, the account that owns it and that account's
// facts, from one read of prefs.js and user.js through C0c's reader. The account is the input's
// own when it is a store folder; otherwise the nearest folder above the input, below the
// profile, that an account owns (TC-11: such an input is a part of that account). Only an input
// no account uses lets the search go up: one two accounts could own, or a profile whose files
// cannot be read with certainty, stops it with no account.
//
// For an imap account the limits are read with their types checked: a value of a type
// Thunderbird never writes for the setting gives no account and a *LimitsError.
func ReadStoreFacts(profileDir, input string) (StoreFacts, error) {
	s, _, err := readPrefs(profileDir)
	if err != nil {
		return StoreFacts{}, err
	}
	profile, err := filepath.Abs(profileDir)
	if err != nil {
		return StoreFacts{}, err
	}
	in, err := filepath.Abs(input)
	if err != nil {
		return StoreFacts{}, err
	}
	goos := runtime.GOOS
	start, isDir := in, true
	if fi, err := os.Stat(in); err == nil && !fi.IsDir() {
		start, isDir = filepath.Dir(in), false
	}
	var acct *Account
	store := start
	for cand := start; ; {
		acct, err = matchAccountIn(s, profile, cand, goos)
		if acct != nil {
			store = cand
			break
		}
		var me *matchError
		if !errors.As(err, &me) || me.kind != NoAccountNone {
			break // ambiguous, no list, or a file that cannot be read with certainty: never climb past it
		}
		parent := filepath.Dir(cand)
		if parent == cand || samePath(parent, profile, goos) || !strings.HasPrefix(parent, profile) {
			break
		}
		cand = parent
	}
	f := StoreFacts{Store: store, Whole: acct != nil && isDir && store == in}
	if acct == nil {
		return f, err
	}
	f.Gmail = gmailOf(acct, s.vals)
	if acct.Type == "imap" {
		lim, lerr := limitsOf(acct.ServerKey, s.vals)
		if lerr != nil {
			return StoreFacts{Store: store}, lerr
		}
		f.Limits = lim
	}
	f.Account = acct
	for _, key := range liveServers(func(k string) string { return s.vals[k] }) {
		if key == acct.ServerKey {
			// The account's own settings are never "another store": its `directory` may still
			// name the profile's old place after a move, the very case tail mode is for (the C2
			// pass's ADV-C2-07).
			continue
		}
		p := func(field string) string { return unquote(s.vals["mail.server."+key+"."+field]) }
		var folders []string
		if rel, ok := strings.CutPrefix(p("directory-rel"), "[ProfD]"); ok {
			folders = append(folders, profile+"/"+rel)
		}
		if dir := p("directory"); dir != "" {
			folders = append(folders, dir)
		}
		for _, d := range folders {
			if !samePath(d, store, goos) {
				f.OtherStores = append(f.OtherStores, d)
			}
		}
	}
	return f, nil
}

// limitsOf reads the account key's limits from vals, each setting with its type checked.
func limitsOf(key string, vals map[string]string) (Limits, error) {
	get := func(field string) (string, string, bool) {
		for _, k := range []string{"mail.server." + key + "." + field, "mail.server.default." + field} {
			if v, ok := vals[k]; ok {
				return k, v, true
			}
		}
		return "", "", false
	}
	intOf := func(field string, def int) (int, error) {
		k, v, ok := get(field)
		if !ok {
			return def, nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, &LimitsError{Setting: k, What: "a value that is not a whole number"}
		}
		return n, nil
	}
	boolOf := func(field string, def bool) (bool, error) {
		k, v, ok := get(field)
		switch {
		case !ok:
			return def, nil
		case v == "true":
			return true, nil
		case v == "false":
			return false, nil
		}
		return false, &LimitsError{Setting: k, What: "a value that is not true or false"}
	}
	var l Limits
	var err error
	if l.MaxAgeDays, err = intOf(limitMaxAge, defMaxAgeDays); err != nil {
		return Limits{}, err
	}
	if l.SizeLimit, err = boolOf(limitSizeOn, false); err != nil {
		return Limits{}, err
	}
	if l.MaxSizeKB, err = intOf(limitMaxSize, defMaxSizeKB); err != nil {
		return Limits{}, err
	}
	background, err := boolOf(limitBackgroundD, true)
	if err != nil {
		return Limits{}, err
	}
	l.NoBackgroundDownload = !background
	return l, nil
}
