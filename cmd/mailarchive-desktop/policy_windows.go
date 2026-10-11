//go:build windows

package main

import "golang.org/x/sys/windows/registry"

// workspacePolicyOff reads the machine policy that turns the Extraction workspace off for
// every user of this computer (extraction design ruling Q6, MA-526): the DWORD
// DisableExtractionWorkspace = 1 under HKLM\SOFTWARE\Policies\MailArchive\Desktop. HKLM
// needs an administrator to write, so a standard user cannot undo it. A missing key or
// value is "not set".
func workspacePolicyOff() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\MailArchive\Desktop`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("DisableExtractionWorkspace")
	return err == nil && v == 1
}
