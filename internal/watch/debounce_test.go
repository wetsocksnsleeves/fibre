package watch

import (
	"slices"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

func TestDebouncerWaitsForQuiet(t *testing.T) {
	d := NewDebouncer(500 * time.Millisecond)
	d.Event("/d/a", at(0))
	if due := d.Due(at(499)); len(due) != 0 {
		t.Errorf("due before the window = %q", due)
	}
	if due := d.Due(at(500)); !slices.Equal(due, []string{"/d/a"}) {
		t.Errorf("due at the window = %q", due)
	}
	if due := d.Due(at(2000)); len(due) != 0 {
		t.Errorf("path came due twice: %q", due)
	}
}

func TestDebouncerPartialWritesActOnce(t *testing.T) {
	// An app writing in chunks keeps the path busy; it is acted on once,
	// a window after the last write.
	d := NewDebouncer(500 * time.Millisecond)
	for _, ms := range []int{0, 200, 400, 600} {
		d.Event("/d/big.json", at(ms))
		if due := d.Due(at(ms)); len(due) != 0 {
			t.Fatalf("due mid-write at %dms: %q", ms, due)
		}
	}
	if due := d.Due(at(1099)); len(due) != 0 {
		t.Errorf("due before the last write was quiet = %q", due)
	}
	if due := d.Due(at(1100)); !slices.Equal(due, []string{"/d/big.json"}) {
		t.Errorf("due = %q", due)
	}
}

func TestDebouncerPathsAreIndependent(t *testing.T) {
	d := NewDebouncer(500 * time.Millisecond)
	d.Event("/d/a", at(0))
	d.Event("/d/b", at(300))
	if due := d.Due(at(600)); !slices.Equal(due, []string{"/d/a"}) {
		t.Errorf("due at 600ms = %q", due)
	}
	if due := d.Due(at(800)); !slices.Equal(due, []string{"/d/b"}) {
		t.Errorf("due at 800ms = %q", due)
	}
}

func TestDebouncerNext(t *testing.T) {
	d := NewDebouncer(500 * time.Millisecond)
	if _, ok := d.Next(); ok {
		t.Error("Next ok with nothing pending")
	}
	d.Event("/d/b", at(300))
	d.Event("/d/a", at(100))
	if next, ok := d.Next(); !ok || !next.Equal(at(600)) {
		t.Errorf("Next = %v, %v; want %v", next, ok, at(600))
	}
}

func TestDebouncerHoldsEventsWhileLocked(t *testing.T) {
	d := NewDebouncer(500 * time.Millisecond)
	d.SetLocked(true)
	d.Event("/d/a", at(0))
	if due := d.Due(at(5000)); len(due) != 0 {
		t.Errorf("due while locked = %q", due)
	}
	if _, ok := d.Next(); ok {
		t.Error("Next ok while locked")
	}
	d.SetLocked(false)
	if due := d.Due(at(5000)); !slices.Equal(due, []string{"/d/a"}) {
		t.Errorf("due after unlock = %q", due)
	}
}

func TestDebouncerEventsDuringLockStillNeedQuiet(t *testing.T) {
	d := NewDebouncer(500 * time.Millisecond)
	d.SetLocked(true)
	d.Event("/d/a", at(1000))
	d.SetLocked(false)
	if due := d.Due(at(1200)); len(due) != 0 {
		t.Errorf("due before quiet = %q", due)
	}
	if due := d.Due(at(1500)); !slices.Equal(due, []string{"/d/a"}) {
		t.Errorf("due = %q", due)
	}
}
