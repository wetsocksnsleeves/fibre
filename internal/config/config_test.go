package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testEnv = Env{
	Home: "/Users/me",
	Lookup: func(key string) (string, bool) {
		v, ok := map[string]string{
			"HOME":            "/Users/me",
			"XDG_CONFIG_HOME": "/Users/me/.config",
		}[key]
		return v, ok
	},
}

func TestParseDest(t *testing.T) {
	tests := []struct {
		name, yaml, want string
	}{
		{"absolute", "dest: /etc/foo", "/etc/foo"},
		{"tilde alone", "dest: ~", "/Users/me"},
		{"tilde prefix", "dest: ~/.claude", "/Users/me/.claude"},
		{"dollar var", "dest: $HOME/.claude", "/Users/me/.claude"},
		{"braced var", "dest: ${XDG_CONFIG_HOME}/nvim", "/Users/me/.config/nvim"},
		{"cleaned", "dest: $HOME/.config//nvim/", "/Users/me/.config/nvim"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.yaml), testEnv)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Dest != tt.want {
				t.Errorf("Dest = %q, want %q", cfg.Dest, tt.want)
			}
		})
	}
}

func TestParseFields(t *testing.T) {
	cfg, err := Parse([]byte("dest: /d\nstrict: true\nexclude:\n  - history.jsonl\n  - projects/**\n"), testEnv)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Strict {
		t.Error("Strict = false, want true")
	}
	if got := strings.Join(cfg.Exclude, ","); got != "history.jsonl,projects/**" {
		t.Errorf("Exclude = %q", got)
	}
}

func TestParseStrictDefaultsFalse(t *testing.T) {
	cfg, err := Parse([]byte("dest: /d"), testEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Strict {
		t.Error("Strict = true, want false")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, yaml, wantErr string
	}{
		{"empty file", "", "dest is required"},
		{"missing dest", "strict: true", "dest is required"},
		{"null dest", "dest:", "dest is required"},
		{"empty dest", "dest: ''", "dest is required"},
		{"list dest", "dest: [a]", "dest must be a string"},
		{"relative dest", "dest: dotfiles/claude", "must be an absolute path"},
		{"undefined var", "dest: $NOPE/x", "undefined variable $NOPE"},
		{"undefined var makes relative", "dest: ${NOPE}", "undefined variable $NOPE"},
		{"tilde user", "dest: ~bob/x", "not ~user"},
		{"unknown key", "dest: /d\nexlude: [a]", "exlude"},
		{"invalid glob", "dest: /d\nexclude: ['[a']", "invalid glob"},
		{"empty pattern", "dest: /d\nexclude: ['']", "empty pattern"},
		{"absolute pattern", "dest: /d\nexclude: [/a]", "leading /"},
		{"bad yaml", "dest: /d\nexclude: [", "yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml), testEnv)
			if err == nil {
				t.Fatalf("Parse succeeded, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestExcluded(t *testing.T) {
	cfg, err := Parse([]byte(`
dest: /d
exclude:
  - history.jsonl
  - projects/**
  - cache
  - "**/*.log"
  - "agents/*.tmp"
`), testEnv)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		rel  string
		want bool
	}{
		{"fibre.yaml", true},
		{"./fibre.yaml", true},
		{"agents/fibre.yaml", false},
		{"history.jsonl", true},
		{"agents/history.jsonl", false},
		{"projects", true},
		{"projects/a.json", true},
		{"projects/x/y/z.json", true},
		{"projectsx/a.json", false},
		{"cache/a/b", true},
		{"debug.log", true},
		{"a/b/debug.log", true},
		{"agents/draft.tmp", true},
		{"agents/sub/draft.tmp", false},
		{"settings.json", false},
		{"agents/reviewer.md", false},
		{"settings.json.fibre-bak", true},
		{"agents/a.md.fibre-bak.3", true},
		{"a.fibre-tmp", true},
		{"a.fibre-bak.x", false},
		{"a.fibre-bak.", false},
		{"fibre-bak", false},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			if got := cfg.Excluded(tt.rel); got != tt.want {
				t.Errorf("Excluded(%q) = %v, want %v", tt.rel, got, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("dest: ~/.claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir, testEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Dest != "/Users/me/.claude" {
		t.Errorf("Dest = %q", cfg.Dest)
	}
}

func TestLoadErrorNamesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p, []byte("strict: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir, testEnv)
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Errorf("err = %v, want it to name %s", err, p)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(t.TempDir(), testEnv); !os.IsNotExist(err) {
		t.Errorf("err = %v, want not-exist", err)
	}
}

func TestAddExcludes(t *testing.T) {
	tests := []struct {
		name, yaml string
		add        []string
		want       string
		added      []string
	}{
		{
			"appends to the list",
			"dest: $HOME/.claude\nexclude:\n  - history.jsonl\n",
			[]string{"todos/**", "history.jsonl"},
			"dest: $HOME/.claude\nexclude:\n  - history.jsonl\n  - todos/**\n",
			[]string{"todos/**"},
		},
		{
			"adds the key",
			"# my set\ndest: ~/.claude\nstrict: true\n",
			[]string{"a.json", "b.json"},
			"# my set\ndest: ~/.claude\nstrict: true\nexclude:\n  - a.json\n  - b.json\n",
			[]string{"a.json", "b.json"},
		},
		{
			"replaces an empty key",
			"dest: ~/.claude\nexclude:\n",
			[]string{"a.json"},
			"dest: ~/.claude\nexclude:\n  - a.json\n",
			[]string{"a.json"},
		},
		{
			"turns a flow list into a block list",
			"dest: ~/.claude\nexclude: [a.json]\n",
			[]string{"b.json"},
			"dest: ~/.claude\nexclude:\n  - a.json\n  - b.json\n",
			[]string{"b.json"},
		},
		{
			"leaves the file alone when nothing is new",
			"dest:   ~/.claude\nexclude: [a.json]\n",
			[]string{"a.json"},
			"dest:   ~/.claude\nexclude: [a.json]\n",
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, added, err := AddExcludes([]byte(tt.yaml), tt.add)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
			if strings.Join(added, ",") != strings.Join(tt.added, ",") {
				t.Errorf("added = %q, want %q", added, tt.added)
			}
		})
	}
}

func TestAddExcludesRejectsBadPatterns(t *testing.T) {
	for _, p := range []string{"", "/abs", "[unclosed"} {
		if _, _, err := AddExcludes([]byte("dest: ~\n"), []string{p}); err == nil {
			t.Errorf("AddExcludes(%q) = nil error", p)
		}
	}
	if _, _, err := AddExcludes([]byte("dest: ~\nexclude: a\n"), []string{"b"}); err == nil {
		t.Error("AddExcludes with a scalar exclude = nil error")
	}
}
