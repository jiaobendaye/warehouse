package desktop_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/jiaobendaye/warehouse/internal/desktop"
)

func TestAcquireInstanceLock_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.lock")

	l, err := desktop.AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer l.Release()
}

func TestAcquireInstanceLock_SecondFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.lock")

	l1, err := desktop.AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer l1.Release()

	_, err = desktop.AcquireInstanceLock(path)
	if !errors.Is(err, desktop.ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
}
