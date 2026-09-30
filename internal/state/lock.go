package state

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked is returned by Lock when another process holds the lock.
var ErrLocked = errors.New("another fibre command is running")

// Lock is an exclusive lock on the state directory, held by CLI commands
// that change links or state. The OS releases it if the process exits.
type Lock struct {
	f *os.File
}

// Acquire takes the lock in dir without waiting.
func Acquire(dir string) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release releases the lock.
func (l *Lock) Release() error {
	return l.f.Close()
}
