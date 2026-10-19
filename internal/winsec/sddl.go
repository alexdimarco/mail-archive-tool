// Package winsec holds the Windows security-descriptor logic shared by the
// schedule-location guard (Q20/MA-649) and the headless job-file owner-only check
// (MA-640/R12): the pure SDDL predicate, tested on any OS with synthetic input,
// and — on Windows only — the DACL read that feeds it (lab L-W15). It imports no
// other internal package, so both internal/schedule and internal/job can use it
// without an import cycle.
package winsec

import (
	"strconv"
	"strings"
)

// DACLWritableByOthers reports whether the DACL in an SDDL security descriptor
// grants a write-class right to any principal other than the owner, SYSTEM, the
// Administrators group or CREATOR OWNER — i.e. whether another account could
// replace or alter the object. Pure and off any build tag so the whole decision
// is unit-tested on any OS. Fail-closed: an SDDL with no readable DACL, or an ACE
// whose rights cannot be shown read-only, counts as writable (refuse rather than
// wave through).
func DACLWritableByOthers(sddl, ownerSid string) bool {
	i := strings.Index(sddl, "D:")
	if i < 0 {
		return true // no DACL section: cannot show it is safe
	}
	dacl := sddl[i+2:]
	if s := strings.Index(dacl, "S:"); s >= 0 { // drop any trailing SACL section
		dacl = dacl[:s]
	}
	if strings.Contains(dacl, "NO_ACCESS_CONTROL") { // a NULL DACL grants everyone: unsafe
		return true
	}
	for {
		open := strings.IndexByte(dacl, '(')
		if open < 0 {
			return false
		}
		closeRel := strings.IndexByte(dacl[open:], ')')
		if closeRel < 0 {
			return false
		}
		ace := dacl[open+1 : open+closeRel]
		dacl = dacl[open+closeRel+1:]
		f := strings.Split(ace, ";")
		if len(f) < 6 {
			continue
		}
		aceType, rights, sid := f[0], f[2], f[5]
		if aceType != "A" && aceType != "OA" { // only ALLOW aces grant access
			continue
		}
		if !rightsIncludeWrite(rights) {
			continue
		}
		if !sidIsSelfOrAdmin(sid, ownerSid) {
			return true
		}
	}
}

// sidIsSelfOrAdmin reports whether an SDDL account field is the owner, or a
// built-in administrative principal that already controls the machine, so a write
// grant to it is not the threat. Everything else (Authenticated Users "AU",
// Everyone "WD", Users "BU", a named user/group SID, …) is another account.
func sidIsSelfOrAdmin(sid, ownerSid string) bool {
	if sid != "" && sid == ownerSid {
		return true
	}
	switch sid {
	// SY=Local System, BA=Builtin Administrators, CO=Creator Owner, LA=local
	// Administrator, LS/NS=Local/Network Service (system principals).
	case "SY", "BA", "CO", "LA", "LS", "NS":
		return true
	}
	return false
}

// rightsIncludeWrite reports whether an SDDL rights field grants any write-class
// right. The field is either a 0x hex access mask or a run of two-letter
// abbreviations. Read/execute-only rights are safe; anything that cannot be shown
// read-only is treated as a write (fail-closed).
func rightsIncludeWrite(rights string) bool {
	if strings.HasPrefix(rights, "0x") || strings.HasPrefix(rights, "0X") {
		v, err := strconv.ParseUint(rights[2:], 16, 64)
		if err != nil {
			return true // unparseable mask: fail-closed
		}
		return v&writeMask != 0
	}
	// Abbreviation run: split into 2-char codes; safe only if every code is a
	// known read/execute-or-inert one.
	readOnly := map[string]bool{
		"FR": true, "FX": true, "GR": true, "GX": true, "RC": true, "RD": true, "RP": true, "LC": true,
	}
	for i := 0; i+2 <= len(rights); i += 2 {
		if !readOnly[rights[i:i+2]] {
			return true // a non-read-only (or unknown) code: treat as write
		}
	}
	return false
}

// writeMask is the set of access-mask bits that let the holder change a file or
// directory: write/append data, write EA/attributes, delete, delete child, write
// DAC/owner, and the generic write/all rights.
const writeMask = 0x0002 | // FILE_WRITE_DATA / FILE_ADD_FILE
	0x0004 | // FILE_APPEND_DATA / FILE_ADD_SUBDIRECTORY
	0x0010 | // FILE_WRITE_EA
	0x0040 | // FILE_DELETE_CHILD
	0x0100 | // FILE_WRITE_ATTRIBUTES
	0x10000 | // DELETE
	0x40000 | // WRITE_DAC
	0x80000 | // WRITE_OWNER
	0x10000000 | // GENERIC_ALL
	0x40000000 // GENERIC_WRITE
