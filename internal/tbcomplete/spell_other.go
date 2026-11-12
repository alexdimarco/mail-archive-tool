//go:build !windows

package tbcomplete

// osSpellings: macOS and Linux give a path no other spelling (Windows' long and 8.3 short names
// are spell_windows.go's).
func osSpellings(string) ([]string, error) { return nil, nil }
