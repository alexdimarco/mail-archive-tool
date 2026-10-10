//go:build !unix

package server

import "os"

// openFlags is what /files/ opens a file with. Windows opens a named pipe as a
// plain file here and has no FIFO to swap in.
const openFlags = os.O_RDONLY
