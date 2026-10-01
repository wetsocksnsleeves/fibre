// Package watch turns filesystem events into reconcile plans. Everything in
// it is pure: time is passed in, and snapshots come from the caller, so the
// rules can be tested without sleeping or touching the filesystem.
package watch

import (
	"sort"
	"time"
)

// Debouncer holds event paths until each has been quiet for a window, so a
// file written in chunks or saved by delete-then-create is acted on once, in
// its final state.
//
// While the CLI lock is held, paths are still recorded but none are due.
// Once it is released they come due as usual; changes the CLI made
// reconcile to nothing, and a user's change made meanwhile is not lost.
type Debouncer struct {
	window  time.Duration
	pending map[string]time.Time // path -> time of its latest event
	locked  bool
}

// NewDebouncer returns a Debouncer with the given quiet window.
func NewDebouncer(window time.Duration) *Debouncer {
	return &Debouncer{window: window, pending: map[string]time.Time{}}
}

// Event records an event at path.
func (d *Debouncer) Event(path string, now time.Time) {
	d.pending[path] = now
}

// SetLocked records whether the CLI lock is held.
func (d *Debouncer) SetLocked(locked bool) {
	d.locked = locked
}

// Due returns, sorted, the paths that have been quiet for the window, and
// forgets them.
func (d *Debouncer) Due(now time.Time) []string {
	if d.locked {
		return nil
	}
	var due []string
	for p, last := range d.pending {
		if now.Sub(last) >= d.window {
			due = append(due, p)
			delete(d.pending, p)
		}
	}
	sort.Strings(due)
	return due
}

// Next returns when the next path comes due. ok is false when nothing is
// pending or the lock is held.
func (d *Debouncer) Next() (at time.Time, ok bool) {
	if d.locked {
		return time.Time{}, false
	}
	for _, last := range d.pending {
		if t := last.Add(d.window); !ok || t.Before(at) {
			at, ok = t, true
		}
	}
	return at, ok
}
