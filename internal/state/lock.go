package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrLocked is returned when another process holds the lock.
var ErrLocked = errors.New("another rivet command is running")

// Lock is an exclusive lock on a file in the state directory. The OS
// releases it if the process exits.
type Lock struct {
	f *os.File
}

// TryAcquire takes the CLI lock in dir without waiting. CLI commands that
// change links or state hold it, and so does the watcher while it applies
// a plan.
func TryAcquire(dir string) (*Lock, error) {
	return tryLock(dir, "lock")
}

// Acquire takes the CLI lock, waiting up to wait for another holder (such as
// the watcher applying a change) to release it.
func Acquire(dir string, wait time.Duration) (*Lock, error) {
	deadline := time.Now().Add(wait)
	for {
		l, err := TryAcquire(dir)
		if !errors.Is(err, ErrLocked) || !time.Now().Before(deadline) {
			return l, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Release releases the lock.
func (l *Lock) Release() error {
	return l.f.Close()
}

// ErrWatcherRunning is returned by AcquireWatcher when a watcher is running.
var ErrWatcherRunning = errors.New("the watcher is already running")

// AcquireWatcher takes the watcher lock, held for the watcher's lifetime so
// only one runs, and records the process id in it.
func AcquireWatcher(dir string) (*Lock, error) {
	l, err := tryLock(dir, "watcher.lock")
	if errors.Is(err, ErrLocked) {
		if pid, running, _ := WatcherPID(dir); running {
			return nil, fmt.Errorf("%w (pid %d)", ErrWatcherRunning, pid)
		}
		return nil, ErrWatcherRunning
	}
	if err != nil {
		return nil, err
	}
	if err := l.f.Truncate(0); err != nil {
		l.Release()
		return nil, err
	}
	if _, err := l.f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		l.Release()
		return nil, err
	}
	return l, nil
}

// WatcherPID reports whether a watcher holds the watcher lock, and its pid.
func WatcherPID(dir string) (pid int, running bool, err error) {
	f, err := os.Open(filepath.Join(dir, "watcher.lock"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		return 0, false, nil // nobody holds it
	} else if !errors.Is(err, syscall.EWOULDBLOCK) {
		return 0, false, err
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return 0, true, err
	}
	pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	return pid, true, nil
}

func tryLock(dir, name string) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDWR|os.O_CREATE, 0o644)
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
