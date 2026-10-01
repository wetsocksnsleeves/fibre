// Package state reads and writes rivet's per-machine state: which sets are
// linked, where, and the manifest of links rivet created for each.
package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const fileName = "state.yaml"

// State is the contents of state.yaml.
type State struct {
	// Root is the absolute path of the dotfiles root the linked sets belong
	// to.
	Root   string          `yaml:"root,omitempty"`
	Linked map[string]*Set `yaml:"linked,omitempty"`
}

// Set is one linked set.
type Set struct {
	Dest     string    `yaml:"dest"`
	LinkedAt time.Time `yaml:"linked_at"`
	// Links is the manifest: slash-separated paths, relative to Dest, that
	// rivet has linked. Sorted.
	Links []string `yaml:"links"`
}

// DefaultDir is $XDG_STATE_HOME/rivet, or ~/.local/state/rivet.
func DefaultDir() (string, error) {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "rivet"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "rivet"), nil
}

// Load reads the state in dir. A missing file is an empty state.
func Load(dir string) (*State, error) {
	p := filepath.Join(dir, fileName)
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return &State{Linked: map[string]*Set{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if s.Linked == nil {
		s.Linked = map[string]*Set{}
	}
	return &s, nil
}

// Save writes s to dir atomically: it writes a temp file in dir and renames
// it over state.yaml, so a reader never sees a partial file.
func Save(dir string, s *State) error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, fileName+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, fileName))
}
