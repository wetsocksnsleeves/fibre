// Package root finds and creates dotfiles roots. A root is a directory
// containing a .rivet marker file.
package root

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Marker is the file that marks a directory as a dotfiles root.
const Marker = ".rivet"

// ErrNotFound is returned by Find when no root contains the start directory.
var ErrNotFound = errors.New("not inside a rivet root (no " + Marker + " found in this directory or any parent; run `rivet init` to create one)")

// Find returns the absolute path of the nearest directory at or above start
// that contains a Marker file.
func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		ok, err := hasMarker(dir)
		if err != nil {
			return "", err
		}
		if ok {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

// Init makes dir a root by creating its Marker file. It reports whether the
// marker was created; false means dir was already a root.
func Init(dir string) (bool, error) {
	ok, err := hasMarker(dir)
	if err != nil || ok {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(dir, Marker), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return false, err
	}
	return true, f.Close()
}

// hasMarker reports whether dir contains a Marker regular file. Anything else
// at that path (such as a directory) is an error rather than a silent miss.
func hasMarker(dir string) (bool, error) {
	path := filepath.Join(dir, Marker)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s exists but is not a regular file", path)
	}
	return true, nil
}
