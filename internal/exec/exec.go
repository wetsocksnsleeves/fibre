// Package exec applies plans to the filesystem.
package exec

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/wetsocksnsleeves/fibre/internal/plan"
)

// ConflictError is returned by Apply for a plan that still has conflicts.
// Nothing has been changed.
type ConflictError struct {
	Conflicts []plan.Action
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%d unresolved conflicts", len(e.Conflicts))
}

// Backup records where a path was moved by an OpBackup.
type Backup struct {
	Path string // relative to dest
	To   string // absolute
}

// Result reports what Apply did beyond the plan itself.
type Result struct {
	Backups []Backup
}

// Apply runs a link plan for the set in setDir and dest, in order. A plan
// with conflicts is refused before anything is touched. If an action fails,
// Apply stops and returns the error; earlier actions are not undone.
func Apply(setDir, dest string, actions []plan.Action) (Result, error) {
	var res Result
	if c := plan.Conflicts(actions); len(c) > 0 {
		return res, &ConflictError{Conflicts: c}
	}
	for _, a := range actions {
		setPath := filepath.Join(setDir, filepath.FromSlash(a.Path))
		destPath := filepath.Join(dest, filepath.FromSlash(a.Path))
		var err error
		switch a.Op {
		case plan.OpMkDir:
			if a.Path == "." {
				err = os.MkdirAll(destPath, 0o755)
			} else {
				err = os.Mkdir(destPath, 0o755)
			}
		case plan.OpLink:
			err = os.Symlink(setPath, destPath)
		case plan.OpReplaceWithLink:
			err = replaceWithLink(setPath, destPath)
		case plan.OpAdopt:
			err = adopt(setPath, destPath)
		case plan.OpBackup:
			var to string
			to, err = backup(destPath)
			if err == nil {
				res.Backups = append(res.Backups, Backup{Path: a.Path, To: to})
			}
		default:
			err = fmt.Errorf("unknown op %v", a.Op)
		}
		if err != nil {
			return res, fmt.Errorf("%s %s: %w", a.Op, destPath, err)
		}
	}
	return res, nil
}

// replaceWithLink swaps the file at destPath for a symlink to setPath in one
// rename, so destPath is never missing.
func replaceWithLink(setPath, destPath string) error {
	tmp := destPath + ".fibre-tmp"
	if err := os.Symlink(setPath, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// adopt moves the file at destPath over setPath, creating setPath's parent
// directories if needed, then links destPath to it.
func adopt(setPath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(setPath), 0o755); err != nil {
		return err
	}
	if err := move(destPath, setPath); err != nil {
		return err
	}
	return os.Symlink(setPath, destPath)
}

// backup renames p to the first free name of p.fibre-bak, p.fibre-bak.1, ...
func backup(p string) (string, error) {
	for i := 0; ; i++ {
		to := p + ".fibre-bak"
		if i > 0 {
			to = fmt.Sprintf("%s.%d", to, i)
		}
		if _, err := os.Lstat(to); errors.Is(err, fs.ErrNotExist) {
			return to, os.Rename(p, to)
		} else if err != nil {
			return "", err
		}
	}
}

// move renames from to to, falling back to copy and remove when they are on
// different filesystems.
func move(from, to string) error {
	err := os.Rename(from, to)
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyFile(from, to); err != nil {
		return err
	}
	return os.Remove(from)
}

func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
