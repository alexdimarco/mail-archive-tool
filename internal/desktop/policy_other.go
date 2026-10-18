//go:build !windows

package desktop

// workspacePolicyOff is the machine policy that turns the Extraction workspace off. Only
// Windows has one (an administrator's registry value); elsewhere the -no-workspace flag
// is the switch.
func workspacePolicyOff() bool { return false }
