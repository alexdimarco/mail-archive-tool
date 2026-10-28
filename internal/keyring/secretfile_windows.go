//go:build windows

package keyring

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/winsec"
)

// ownersOnly says why the open secret file f is not this account's alone, or "":
// its owner must be this user and no other account may read it (§3.4). The decision
// is winsec's; the refusal names the owner and the accounts that can read it, and
// gives the icacls commands that make the file this account's alone, spelled with
// this account's own name so they work as typed in a Command Prompt (the P3-8
// Windows lab, B11; not PowerShell, WINACL-3).
func ownersOnly(f *os.File, path string) string {
	h := windows.Handle(f.Fd())
	err := winsec.CheckSecretFile(h)
	if err == nil {
		return ""
	}
	me, owner, readers, readable, ok := fileAccess(h)
	if !ok {
		return err.Error() + " — use a file only you can read, or type the password instead"
	}
	return secretFileRefusal(path, me, owner, readers, readable)
}

// account is a Windows account by its name (DOMAIN\name, or "" when it cannot be
// looked up) and its SID.
type account struct{ name, sid string }

// shown is the account as a person reads it.
func (a account) shown() string {
	if a.name != "" {
		return a.name
	}
	return "an account with no name here (" + a.sid + ")"
}

// arg is the account as icacls takes it: its name, or its SID marked with "*".
func (a account) arg() string {
	if a.name != "" {
		return a.name
	}
	return "*" + a.sid
}

// accountOf looks up the account an SDDL account field (a SID or an alias such as
// "BU") names.
func accountOf(field string) account {
	sid, err := windows.StringToSid(field)
	if err != nil {
		return account{sid: field}
	}
	a := account{sid: sid.String()}
	if name, domain, _, err := sid.LookupAccount(""); err == nil && name != "" {
		a.name = name
		if domain != "" {
			a.name = domain + `\` + name
		}
	}
	return a
}

// reader is an account the file's access list lets read it; inherited says the entry
// is passed down from its folder rather than set on the file.
type reader struct {
	account
	inherited bool
}

// fileAccess reads who the open file h belongs to and who else can read it: this
// process's account, the owner when it is another account (else nil), each account
// an entry lets read it (judged entry by entry with winsec's own rule), and whether
// winsec judges the access list as a whole readable by others.
func fileAccess(h windows.Handle) (me account, owner *account, readers []reader, readable, ok bool) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return account{}, nil, nil, false, false
	}
	me = accountOf(tu.User.Sid.String())
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return me, nil, nil, false, false
	}
	owner, readers, readable = accessOf(sd, me)
	return me, owner, readers, readable, true
}

// accessOf is fileAccess's reading of a security descriptor for the account me. An
// entry is me's own when its account field names me — by SID, or by an alias such as
// "LA", which Windows writes for the built-in Administrator — so me's own entry is
// never listed as another reader, nor removed by the remedy (the P3-8 fixes' pass,
// WINACL-1).
func accessOf(sd *windows.SECURITY_DESCRIPTOR, me account) (owner *account, readers []reader, readable bool) {
	if o, _, err := sd.Owner(); err != nil || o == nil {
		owner = &account{name: "an account Windows does not name"}
	} else if o.String() != me.sid {
		a := accountOf(o.String())
		owner = &a
	}
	isMe := winsec.UserEntry(me.sid)
	sddl := sd.String()
	readable = winsec.DACLReadableByOthersFor(sddl, isMe)
	i := strings.Index(sddl, "D:")
	if i < 0 {
		return owner, nil, readable
	}
	dacl := sddl[i+2:]
	if s := strings.Index(dacl, "S:"); s >= 0 {
		dacl = dacl[:s]
	}
	seen := map[string]bool{}
	for _, ace := range topLevelEntries(dacl) {
		f := strings.Split(ace, ";")
		if len(f) < 6 || !winsec.DACLReadableByOthersFor("D:("+ace+")", isMe) {
			continue
		}
		r := reader{account: accountOf(f[5]), inherited: strings.Contains(f[1], "ID")}
		key := fmt.Sprint(r.sid, r.inherited)
		if !seen[key] {
			seen[key] = true
			readers = append(readers, r)
		}
	}
	return owner, readers, readable
}

// topLevelEntries splits an SDDL access list into its entries at their top-level
// parentheses, so a conditional entry's own parentheses stay inside it.
func topLevelEntries(dacl string) []string {
	var out []string
	depth, start := 0, -1
	for i, r := range dacl {
		switch r {
		case '(':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case ')':
			if depth--; depth == 0 && start >= 0 {
				out = append(out, dacl[start:i])
			}
		}
	}
	return out
}

// notPowerShell says why the commands are for a Command Prompt only: PowerShell reads
// $ and ` inside double quotes, so an account or folder name holding one would name
// another account or path there (the P3-8 fixes' pass, WINACL-3).
const notPowerShell = "a Command Prompt (not PowerShell, which reads $ and ` inside quotes as something else)"

// secretFileRefusal says what is wrong with the secret file at path — another
// account owns it, other accounts can read it — naming them, and the commands that
// make it this account's alone: /setowner for an owner that is not me (nothing else
// changes the owner), then, when others can read it, one icacls that drops what the
// folder passes down, replaces my own entry with full control and removes each other
// account's entry set on the file itself. Running them ends the refusal.
func secretFileRefusal(path string, me account, owner *account, readers []reader, readable bool) string {
	var why []string
	if owner != nil {
		why = append(why, fmt.Sprintf("is owned by %s, not by you (%s)", owner.shown(), me.shown()))
	}
	var names, remove []string
	for _, r := range readers {
		names = append(names, r.shown())
		if !r.inherited && r.sid != me.sid { // never this account's own entry (WINACL-1)
			remove = append(remove, `"`+r.arg()+`"`)
		}
	}
	readable = readable || len(readers) > 0 || owner == nil
	if len(names) > 0 {
		why = append(why, "can be read by other accounts: "+strings.Join(names, ", "))
	} else if readable {
		why = append(why, "can be read by other accounts")
	}
	q := `"` + path + `"`
	var cmds []string
	if owner != nil {
		cmds = append(cmds, fmt.Sprintf(`icacls %s /setowner "%s"`, q, me.arg()))
	}
	if readable {
		acl := fmt.Sprintf(`icacls %s /inheritance:r /grant:r "%s:F"`, q, me.arg())
		if len(remove) > 0 {
			acl += " /remove:g " + strings.Join(remove, " ")
		}
		cmds = append(cmds, acl)
	}
	var b strings.Builder
	b.WriteString(strings.Join(why, ", and "))
	if len(cmds) == 1 {
		b.WriteString(". To make it yours alone, run this in " + notPowerShell + ", then try again:")
	} else {
		b.WriteString(". To make it yours alone, run these in " + notPowerShell + ", one after the other, then try again:")
	}
	for _, c := range cmds {
		b.WriteString("\n  " + c)
	}
	if owner != nil {
		them := "them"
		if len(cmds) == 1 {
			them = "it"
		}
		b.WriteString("\nIf Windows answers \"Access is denied\", run " + them + " in a window opened with \"Run as administrator\".")
	}
	return b.String()
}
