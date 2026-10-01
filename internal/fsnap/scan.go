package fsnap

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Scan snapshots the whole tree under dir. Paths for which skip returns true
// are left out; a skipped directory is not descended into.
func Scan(dir string, skip func(rel string) bool) (Tree, error) {
	return scan(dir, skip, true)
}

// ScanKinds is Scan without hashing file contents: File entries have a zero
// Sum. It is for finding what exists in a large tree cheaply.
func ScanKinds(dir string, skip func(rel string) bool) (Tree, error) {
	return scan(dir, skip, false)
}

func scan(dir string, skip func(rel string) bool, hash bool) (Tree, error) {
	tree := Tree{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && skip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !hash && info.Mode().IsRegular() {
			tree[rel] = Entry{Kind: File}
			return nil
		}
		e, err := entry(p, info)
		if err != nil {
			return err
		}
		tree[rel] = e
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tree, nil
}

// Lookup snapshots dir itself (".") and each of rels, without following
// symlinks at those paths. Paths with nothing at them are left out, including
// paths below a file.
func Lookup(dir string, rels []string) (Tree, error) {
	tree := Tree{}
	for _, rel := range append([]string{"."}, rels...) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		info, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			continue
		}
		if err != nil {
			return nil, err
		}
		e, err := entry(p, info)
		if err != nil {
			return nil, err
		}
		tree[rel] = e
	}
	return tree, nil
}

func entry(p string, info fs.FileInfo) (Entry, error) {
	switch mode := info.Mode(); {
	case mode.IsDir():
		return Entry{Kind: Dir}, nil
	case mode&fs.ModeSymlink != 0:
		target, err := os.Readlink(p)
		if err != nil {
			return Entry{}, err
		}
		return Entry{Kind: Symlink, Target: target}, nil
	case mode.IsRegular():
		sum, err := hashFile(p)
		if err != nil {
			return Entry{}, err
		}
		return Entry{Kind: File, Sum: sum}, nil
	default:
		return Entry{Kind: Other}, nil
	}
}

func hashFile(p string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	f, err := os.Open(p)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return sum, fmt.Errorf("hashing %s: %w", p, err)
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}
