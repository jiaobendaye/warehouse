//go:build windows

package desktop

import (
	"os"
)

// AcquireInstanceLock uses exclusive file creation as a lock. On Windows,
// os.O_EXCL ensures atomic creation — if the file already exists, another
// instance is running.
func AcquireInstanceLock(path string) (*InstanceLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return &InstanceLock{file: f, path: path}, nil
}