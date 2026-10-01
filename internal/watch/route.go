package watch

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/wetsocksnsleeves/rivet/internal/plan"
)

// Target is a linked set as events are routed to it.
type Target struct {
	Name, SetDir, Dest string
	Excluded           func(rel string) bool
}

// Hit is an event path relative to one set: the same relative path names
// the set file and its place in dest. "." is the whole set.
type Hit struct {
	Set, Rel string
}

// Route maps an absolute event path to every set it concerns: each set whose
// directory or dest contains it, unless that set excludes it. Nested dests
// mean one path can concern several sets; Reconcile decides which one acts.
//
// Paths inside an ignored directory (the root and rivet's state, which may
// sit inside a dest such as $HOME) are only routed to the set they belong
// to, never to a dest that happens to contain them.
func Route(abs string, targets []Target, ignored []string) []Hit {
	inIgnored := false
	for _, d := range ignored {
		if _, ok := relWithin(abs, d); ok {
			inIgnored = true
		}
	}
	var hits []Hit
	for _, t := range targets {
		for _, base := range []string{t.SetDir, t.Dest} {
			if base == t.Dest && inIgnored {
				continue
			}
			rel, ok := relWithin(abs, base)
			if !ok || (rel != "." && t.Excluded(rel)) {
				continue
			}
			hits = append(hits, Hit{Set: t.Name, Rel: rel})
			break
		}
	}
	return hits
}

// Plan reconciles the sets and keeps, for each set that was hit, only the
// actions at or below its hit paths, plus the directories those actions
// need. Sets that were not hit are left out.
func Plan(sets []plan.SetState, hits []Hit) []plan.SetPlan {
	rels := map[string][]string{}
	for _, h := range hits {
		rels[h.Set] = append(rels[h.Set], h.Rel)
	}
	var out []plan.SetPlan
	for _, p := range plan.Reconcile(sets) {
		hit, ok := rels[p.Name]
		if !ok {
			continue
		}
		out = append(out, plan.SetPlan{Name: p.Name, Actions: filter(p.Actions, hit)})
	}
	return out
}

func filter(actions []plan.Action, rels []string) []plan.Action {
	affected := func(p string) bool {
		for _, r := range rels {
			if r == "." || p == r || strings.HasPrefix(p, r+"/") {
				return true
			}
		}
		return false
	}
	keep := make([]bool, len(actions))
	needDir := map[string]bool{}
	for i, a := range actions {
		if a.Op != plan.OpMkDir && affected(a.Path) {
			keep[i] = true
			for d := path.Dir(a.Path); d != "."; d = path.Dir(d) {
				needDir[d] = true
			}
			needDir["."] = true
		}
	}
	var out []plan.Action
	for i, a := range actions {
		if keep[i] || (a.Op == plan.OpMkDir && (needDir[a.Path] || affected(a.Path))) {
			out = append(out, a)
		}
	}
	return out
}

// relWithin returns abs relative to base, slash-separated, if abs is base or
// inside it.
func relWithin(abs, base string) (string, bool) {
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
