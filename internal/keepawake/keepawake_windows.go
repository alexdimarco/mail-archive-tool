//go:build windows

package keepawake

import (
	"runtime"
	"syscall"
)

// Hold keeps Windows from idle sleep until the returned func is called:
// ES_SYSTEM_REQUIRED | ES_CONTINUOUS through SetThreadExecutionState, in process,
// from a goroutine locked to its OS thread for the hold's life (the request
// belongs to the thread that made it). Nothing is shelled out. It does not stop a
// sleep the person asks for (a lid closed, Start > Sleep).
func Hold() (release func()) {
	const (
		esContinuous     = 0x80000000
		esSystemRequired = 0x00000001
	)
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")
	held, done, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer close(stopped)
		proc.Call(uintptr(esContinuous | esSystemRequired))
		close(held)
		<-done
		proc.Call(uintptr(esContinuous)) // clears the request
	}()
	<-held // the request is in place when Hold returns
	return func() {
		close(done)
		<-stopped
	}
}
