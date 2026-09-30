// Package config loads and validates a set's fibre.yaml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// FileName is the set config file at the root of each set. It is always
// excluded from linking and adoption.
const FileName = "fibre.yaml"

// Config is a validated set config. Dest is absolute and cleaned, and every
// exclude pattern is valid.
type Config struct {
	Dest    string
	Exclude []string
	Strict  bool
}

// Env supplies the values used to expand dest.
type Env struct {
	Home   string
	Lookup func(key string) (string, bool)
}

// OSEnv returns an Env backed by the process environment.
func OSEnv() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	return Env{Home: home, Lookup: os.LookupEnv}, nil
}

type file struct {
	// Dest is a node so a bare `dest: ~`, which YAML reads as null, can be
	// told apart from a missing or empty dest.
	Dest    yaml.Node `yaml:"dest"`
	Exclude []string  `yaml:"exclude"`
	Strict  bool      `yaml:"strict"`
}

// Load reads and validates the config of the set in setDir.
func Load(setDir string, env Env) (*Config, error) {
	p := filepath.Join(setDir, FileName)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(data, env)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return cfg, nil
}

// Parse decodes and validates a set config. Unknown keys are rejected so a
// misspelled key (e.g. "exlude") is not silently ignored.
func Parse(data []byte, env Env) (*Config, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	raw, err := destString(f.Dest)
	if err != nil {
		return nil, err
	}
	dest, err := expand(raw, env)
	if err != nil {
		return nil, fmt.Errorf("dest: %w", err)
	}
	if !filepath.IsAbs(dest) {
		return nil, fmt.Errorf("dest %q must be an absolute path (after expanding ~ and variables)", raw)
	}

	for _, p := range f.Exclude {
		if err := validatePattern(p); err != nil {
			return nil, fmt.Errorf("exclude %q: %w", p, err)
		}
	}

	return &Config{Dest: filepath.Clean(dest), Exclude: f.Exclude, Strict: f.Strict}, nil
}

func destString(n yaml.Node) (string, error) {
	switch {
	case n.Kind == yaml.ScalarNode && n.Tag == "!!null" && n.Value == "~":
		return "~", nil
	case n.Kind == 0, n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == ""):
		return "", errors.New("dest is required")
	case n.Kind != yaml.ScalarNode:
		return "", fmt.Errorf("line %d: dest must be a string", n.Line)
	}
	return n.Value, nil
}

// Excluded reports whether rel, a slash-separated path relative to the set or
// dest, is excluded. A path is excluded if it or any of its parent
// directories matches a pattern, so excluding a directory excludes its
// contents. The set's own FileName and the backup and temp files fibre
// creates (*.fibre-bak, *.fibre-bak.N, *.fibre-tmp) are always excluded.
func (c *Config) Excluded(rel string) bool {
	rel = path.Clean(rel)
	if rel == FileName || isFibreFile(path.Base(rel)) {
		return true
	}
	for p := rel; p != "." && p != "/"; p = path.Dir(p) {
		for _, pat := range c.Exclude {
			// Patterns are validated in Parse, so Match cannot fail.
			if ok, _ := doublestar.Match(pat, p); ok {
				return true
			}
		}
	}
	return false
}

func isFibreFile(base string) bool {
	if strings.HasSuffix(base, ".fibre-tmp") || strings.HasSuffix(base, ".fibre-bak") {
		return true
	}
	i := strings.LastIndex(base, ".fibre-bak.")
	if i < 0 {
		return false
	}
	n := base[i+len(".fibre-bak."):]
	return n != "" && strings.Trim(n, "0123456789") == ""
}

func validatePattern(p string) error {
	if p == "" {
		return errors.New("empty pattern")
	}
	if strings.HasPrefix(p, "/") {
		return errors.New("patterns are relative to the set; remove the leading /")
	}
	if !doublestar.ValidatePattern(p) {
		return errors.New("invalid glob")
	}
	return nil
}

// expand replaces a leading ~ with env.Home and $VAR / ${VAR} with their
// values. An undefined variable is an error rather than an empty string.
func expand(s string, env Env) (string, error) {
	if s == "~" || strings.HasPrefix(s, "~/") {
		s = env.Home + s[1:]
	} else if strings.HasPrefix(s, "~") {
		return "", fmt.Errorf("%q: only ~ and ~/ are supported, not ~user", s)
	}

	var missing []string
	out := os.Expand(s, func(key string) string {
		if env.Lookup != nil {
			if v, ok := env.Lookup(key); ok {
				return v
			}
		}
		missing = append(missing, key)
		return ""
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("undefined variable $%s", missing[0])
	}
	return out, nil
}
