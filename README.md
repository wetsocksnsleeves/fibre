![CODED BY](https://img.shields.io/badge/CODED%20BY-LLM-d29922?style=for-the-badge)

# fibre

fibre is a dotfile manager. It symlinks each file in a dotfiles set
into the directory where that file belongs, and a background watcher copies
changes made in that directory back into the set. When an app creates a new
file in `~/.claude`, the file is synced.

# Why

GNU Stow links a whole directory when it can. When it can't, it links the
files inside the directory one by one. Stow falls back to per-file links when:

- the target directory already exists, which is common because apps create
  `~/.config/<app>` or `~/.claude` on first run
- two packages put files in the same directory
- you pass `--no-folding`

After that, a file an app creates in the target is not synced.

fibre always links individual files, and its watcher handles the changes that
links alone miss:

- A new file in the destination is moved into the set and replaced with a
  symlink.
- Many apps save by writing a temp file and renaming it over the original,
  which replaces the symlink with a real file. The watcher copies the new
  contents into the set and restores the symlink.
- Deleting a link in the destination deletes the file from the set. Deleting
  a file from the set removes its link.
- Files added to the set by `git pull` or a branch switch are linked
  automatically.
- When the watcher starts, it reconciles changes made while it was stopped.


# Installation

To install, run:

```sh
curl -fsSL https://raw.githubusercontent.com/wetsocksnsleeves/fibre/main/install.sh | sh
```

# Usage

## Create a dotfiles root

```sh
mkdir ~/.dotfiles && cd ~/.dotfiles
fibre init
```

`fibre init` creates a `.fibre` marker file. Every other command looks for it
in the current directory and its parents, the way git looks for `.git`.

## Add a set

```sh
fibre init claude --dest '$HOME/.claude' --exclude 'history.jsonl,projects/**'
```

This creates `claude/fibre.yaml`. The quotes keep `$HOME` unexpanded in the
file, so the same set works on machines with different home directories.

If `~/.claude` already exists, fibre moves each file that isn't excluded into
`claude/` and links it back. Excluded files stay in `~/.claude`.

## Link a set on another machine

```sh
git clone <your-dotfiles-repo> ~/.dotfiles && cd ~/.dotfiles
fibre link claude
```

If a different file already exists at a target path, `link` lists every
conflict and changes nothing. Run it again with one of these flags:

| Flag | Effect |
|---|---|
| `--adopt` | Move the existing file into the set, overwriting the set's version, then link it. |
| `--force` | Rename the existing file to `<name>.fibre-bak`, then link the set's version. |
| `--skip` | Leave conflicting paths alone and link everything else. |

A real file with the same contents as the set's file is not a conflict. fibre
replaces it with a link.

## Start the watcher

```sh
fibre watch install
```

This registers the watcher as a launchd agent that starts at login, and starts
it now. `fibre watch stop`, `fibre watch start`, `fibre watch status` and
`fibre watch uninstall` manage the agent. `fibre watch run` runs the watcher
in the foreground instead.

## Check status

```sh
fibre status
```

Prints one line per set, then any paths that need attention: conflicts, files
the watcher hasn't processed yet, and untracked files. The last line says
whether the watcher is running. Pass a set name to show only that set, and
`-v` to list every link.

```
claude → ~/.claude   2 linked
  Untracked (new in dest; the watcher will adopt them):
    agents/draft.md

nvim → ~/.config/nvim   1 linked, in sync
```

## Stop managing a set

```sh
fibre unlink claude
```

Replaces each of the set's links with a copy of the file it pointed to. The
files in `~/.claude` keep working with the same contents, but changes no
longer sync in either direction. The set's files stay in the repo.

# fibre.yaml

Each set has a `fibre.yaml` at its top level.

```yaml
dest: $HOME/.claude
exclude:
  - history.jsonl
  - projects/**
strict: false
```

| Key | Meaning |
|---|---|
| `dest` | Required. The directory the set's files are linked into. The set's tree mirrors the tree under `dest`. fibre expands `~` and environment variables. |
| `exclude` | Glob patterns relative to the set and to `dest`. `**` matches any number of directories. `fibre.yaml` is always excluded. |
| `strict` | Defaults to `false`. When `true`, the watcher still restores links after atomic saves but never adopts new files, and `status` doesn't list untracked files. Use it for sets with a broad `dest` like `$HOME`. |

Several sets can share a `dest`. When a new file appears, fibre adopts it into
the non-strict set whose `dest` is the deepest directory containing it, so a
new file in `~/.claude` goes to a `claude` set rather than one with
`dest: $HOME`. If two sets have the same `dest`, fibre leaves the file in
place and `status` lists it as claimed by both.

fibre keeps per-machine state in `~/.local/state/fibre/state.yaml`, outside
the repo. It records which sets are linked on that machine, so each machine
can link a different subset.
