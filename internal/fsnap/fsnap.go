// Package fsnap holds plain-value snapshots of directory trees. Planners read
// snapshots instead of the filesystem, so they can be tested without one.
package fsnap

import (
	"crypto/sha256"
	"path"
)

// Kind is the type of a filesystem entry. Symlinks are not followed.
type Kind int

const (
	File Kind = iota + 1
	Dir
	Symlink
	// Other is anything else, such as a socket or named pipe.
	Other
)

func (k Kind) String() string {
	switch k {
	case File:
		return "file"
	case Dir:
		return "directory"
	case Symlink:
		return "symlink"
	case Other:
		return "special file"
	}
	return "unknown"
}

// Entry is one path in a snapshot.
type Entry struct {
	Kind Kind
	// Target is the symlink's target as stored in the link, for Symlink.
	Target string
	// Sum is the SHA-256 of the contents, for File.
	Sum [sha256.Size]byte
}

// Tree maps slash-separated paths, relative to the snapshot's base directory,
// to entries. The base directory itself is ".". A path with no key has
// nothing at it, or was not looked at: a snapshot of a large destination
// such as $HOME holds only the paths a planner needs.
type Tree map[string]Entry

// Builder builds a Tree for tests.
type Builder struct {
	tree Tree
}

// New returns a Builder whose tree contains the base directory.
func New() *Builder {
	return &Builder{tree: Tree{".": {Kind: Dir}}}
}

// File adds a regular file with the given contents, and any missing parent
// directories.
func (b *Builder) File(rel, contents string) *Builder {
	b.parents(rel)
	b.tree[rel] = Entry{Kind: File, Sum: sha256.Sum256([]byte(contents))}
	return b
}

// Dir adds a directory and any missing parent directories.
func (b *Builder) Dir(rel string) *Builder {
	b.parents(rel)
	b.tree[rel] = Entry{Kind: Dir}
	return b
}

// Symlink adds a symlink pointing at target, and any missing parent
// directories.
func (b *Builder) Symlink(rel, target string) *Builder {
	b.parents(rel)
	b.tree[rel] = Entry{Kind: Symlink, Target: target}
	return b
}

// Tree returns the built tree.
func (b *Builder) Tree() Tree {
	return b.tree
}

func (b *Builder) parents(rel string) {
	for p := path.Dir(rel); p != "."; p = path.Dir(p) {
		if _, ok := b.tree[p]; !ok {
			b.tree[p] = Entry{Kind: Dir}
		}
	}
}
