//go:build windows

package encrecord

import "golang.org/x/sys/windows"

// lookupAccountName is the DOMAIN\name of the account sid names, or "" when it
// cannot be looked up.
func lookupAccountName(sid string) string {
	s, err := windows.StringToSid(sid)
	if err != nil {
		return ""
	}
	name, domain, _, err := s.LookupAccount("")
	if err != nil || name == "" {
		return ""
	}
	if domain != "" {
		return domain + `\` + name
	}
	return name
}
