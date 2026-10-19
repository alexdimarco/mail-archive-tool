//go:build windows

package app

import (
	"runtime"
	"syscall"
)

// holdAwake keeps Windows from idle sleep while a run is active (design §4.15,
// RR2-C8): ES_SYSTEM_REQUIRED | ES_CONTINUOUS through SetThreadExecutionState, in
// process, from a goroutine locked to its OS thread for the run's life (the request
// belongs to the thread that made it). Nothing is shelled out. The returned func
// releases it.
func holdAwake() (release func()) {
	const (
		esContinuous     = 0x80000000
		esSystemRequired = 0x00000001
	)
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer close(stopped)
		proc.Call(uintptr(esContinuous | esSystemRequired))
		<-done
		proc.Call(uintptr(esContinuous)) // clears the request
	}()
	return func() {
		close(done)
		<-stopped
	}
}
