package plan

import "sort"

// Actionable returns the actions an executor can apply without a person
// deciding: everything but conflicts and ties, which are only reported.
func Actionable(actions []Action) []Action {
	var out []Action
	for _, a := range actions {
		if a.Op != OpConflict && a.Op != OpUntracked {
			out = append(out, a)
		}
	}
	return out
}

// UpdateManifest returns links with each of touched re-decided from in,
// which snapshots at least the touched paths after a plan ran: a touched
// path is in the manifest exactly when dest now links it to the set.
// Paths that were not touched keep their manifest entry. Sorted.
func UpdateManifest(links, touched []string, in LinkInput) []string {
	set := map[string]bool{}
	for _, l := range links {
		set[l] = true
	}
	for _, t := range touched {
		delete(set, t)
	}
	linked := map[string]bool{}
	for _, l := range Linked(in) {
		linked[l] = true
	}
	for _, t := range touched {
		if linked[t] {
			set[t] = true
		}
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
