package desktop

import (
	"errors"
	"os"
)

// ErrAlreadyRunning is returned by AcquireInstanceLock when another process
// already holds the lock file.
var ErrAlreadyRunning = errors.New("another instance is already running")

// InstanceLock represents an exclusive lock on a lockfile. Hold it for the
// lifetime of the process.
type InstanceLock struct {
	file *os.File
	path string
}

// Release closes the lock file and removes it.
func (l *InstanceLock) Release() error {
	if l.file == nil {
		return nil
	}
	_ = l.file.Close()
	l.file = nil
	_ = os.Remove(l.path)
	return nil
}