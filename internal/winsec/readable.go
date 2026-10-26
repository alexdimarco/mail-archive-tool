package winsec

import (
	"strconv"
	"strings"
)

// DACLReadableByOthers reports whether the DACL in an SDDL security descriptor lets
// any account other than userSid, SYSTEM or the Administrators group read the
// file's contents — the check a password or recovery-key file must pass
// (design-encrypted-archives-phase3 §3.4). Only allow entries that apply to the
// object itself count (an inherit-only entry does not); a deny entry is never
// counted as making it safe. Pure and off any build tag, so it is unit-tested on any
// OS with synthetic SDDL. Fail-closed: no readable DACL, a NULL DACL, or rights it
// cannot show exclude reading count as readable.
func DACLReadableByOthers(sddl, userSid string) bool {
	i := strings.Index(sddl, "D:")
	if i < 0 {
		return true
	}
	dacl := sddl[i+2:]
	if s := strings.Index(dacl, "S:"); s >= 0 {
		dacl = dacl[:s]
	}
	if strings.Contains(dacl, "NO_ACCESS_CONTROL") {
		return true
	}
	aces, torn := splitACEs(dacl)
	if torn {
		return true // a torn entry: cannot show it is safe
	}
	for _, ace := range aces {
		f := strings.Split(ace, ";")
		if len(f) < 6 {
			return true
		}
		aceType, flags, rights, sid := f[0], f[1], f[2], f[5]
		// Every entry type but a deny or an audit may grant: an allow, an object or
		// callback (conditional) allow, and a type this code does not know (the
		// P3-1a pass, SEC-04).
		if notAGrant[aceType] {
			continue
		}
		if strings.Contains(flags, "IO") { // inherit-only: not this file's access
			continue
		}
		if !rightsIncludeRead(rights) {
			continue
		}
		if userSid != "" && sid == userSid {
			continue
		}
		switch sid {
		case "SY", "S-1-5-18", "BA", "S-1-5-32-544": // SYSTEM, Administrators
			continue
		}
		return true
	}
	return false
}

// notAGrant are the entry types that never grant access: denies and audits.
var notAGrant = map[string]bool{"D": true, "OD": true, "XD": true, "AU": true, "AL": true, "OU": true, "OL": true, "XU": true}

// splitACEs splits a DACL's entries at their top-level parentheses, so a callback
// entry's condition, which has parentheses of its own, stays one entry. torn
// reports parentheses that do not balance.
func splitACEs(dacl string) (aces []string, torn bool) {
	depth, start := 0, -1
	for i, r := range dacl {
		switch r {
		case '(':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case ')':
			depth--
			switch {
			case depth < 0:
				return nil, true
			case depth == 0:
				aces = append(aces, dacl[start:i])
			}
		}
	}
	return aces, depth != 0
}

// readMask is the access-mask bits that read a file's contents, or let the holder
// grant itself that: read data, write the DACL or owner, and the generic read, all
// and maximum-allowed rights.
const readMask = 0x0001 | // FILE_READ_DATA
	0x40000 | // WRITE_DAC
	0x80000 | // WRITE_OWNER
	0x02000000 | // MAXIMUM_ALLOWED
	0x10000000 | // GENERIC_ALL
	0x80000000 // GENERIC_READ

// sddlRights are the SDDL rights abbreviations and their access-mask bits.
var sddlRights = map[string]uint64{
	"GA": 0x10000000, "GR": 0x80000000, "GW": 0x40000000, "GX": 0x20000000,
	"FA": 0x1F01FF, "FR": 0x120089, "FW": 0x120116, "FX": 0x1200A0,
	"RC": 0x20000, "SD": 0x10000, "WD": 0x40000, "WO": 0x80000,
	"CC": 0x1, "DC": 0x2, "LC": 0x4, "SW": 0x8, "RP": 0x10, "WP": 0x20, "DT": 0x40, "LO": 0x80, "CR": 0x100,
	"KA": 0xF003F, "KR": 0x20019, "KW": 0x20006, "KX": 0x20019,
}

// rightsIncludeRead reports whether an SDDL rights field grants a read-class right.
// An unknown abbreviation or an unparseable mask counts as one (fail-closed).
func rightsIncludeRead(rights string) bool {
	var v uint64
	if strings.HasPrefix(rights, "0x") || strings.HasPrefix(rights, "0X") {
		n, err := strconv.ParseUint(rights[2:], 16, 64)
		if err != nil {
			return true
		}
		v = n
	} else {
		if len(rights)%2 != 0 {
			return true
		}
		for i := 0; i+2 <= len(rights); i += 2 {
			bits, ok := sddlRights[rights[i:i+2]]
			if !ok {
				return true
			}
			v |= bits
		}
	}
	return v&readMask != 0
}
