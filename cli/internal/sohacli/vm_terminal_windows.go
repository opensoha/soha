package sohacli

import (
	"context"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/windows"
)

var cancelSynchronousIO = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

func readVMTerminalFile(ctx context.Context, file *os.File, buffer []byte) (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	thread, err := windows.OpenThread(windows.THREAD_TERMINATE, false, windows.GetCurrentThreadId())
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(thread)
	finished, canceled := make(chan struct{}), make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(canceled)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			// Repeat until Read returns: cancellation can race before the synchronous IO starts.
			_, _, _ = cancelSynchronousIO.Call(uintptr(thread))
			select {
			case <-finished:
				return
			case <-ticker.C:
			}
		}
	})
	n, err := file.Read(buffer)
	close(finished)
	if !stop() {
		<-canceled
	}
	return n, err
}
