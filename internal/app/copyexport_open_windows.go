//go:build windows

package app

// openNoBlock is nothing on Windows: a directory cannot hold a named pipe, and a
// device name is refused by os.Root.
const openNoBlock = 0
