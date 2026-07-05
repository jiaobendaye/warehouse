//go:build !windows

package desktop

import (
	"os"
	"syscall"
)

// AcquireInstanceLock attempts to acquire an exclusive, non-blocking lock via
// syscall.Flock (LOCK_EX|LOCK_NB). Used on Linux and macOS.
func AcquireInstanceLock(path string) (*InstanceLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, ErrAlreadyRunning
	}
	return &InstanceLock{file: f, path: path}, nil
}