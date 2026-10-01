package fsnap

import "testing"

func TestBuilderAddsParents(t *testing.T) {
	tree := New().File("a/b/c.txt", "x").Tree()
	for _, p := range []string{".", "a", "a/b"} {
		if tree[p].Kind != Dir {
			t.Errorf("%s: kind = %v, want directory", p, tree[p].Kind)
		}
	}
	if tree["a/b/c.txt"].Kind != File {
		t.Errorf("a/b/c.txt: kind = %v, want file", tree["a/b/c.txt"].Kind)
	}
}

func TestBuilderKeepsExistingParent(t *testing.T) {
	tree := New().File("a", "x").File("a/b", "y").Tree()
	if tree["a"].Kind != File {
		t.Errorf("a: kind = %v, want file", tree["a"].Kind)
	}
}

func TestBuilderSums(t *testing.T) {
	tree := New().File("a", "same").File("b", "same").File("c", "other").Tree()
	if tree["a"].Sum != tree["b"].Sum {
		t.Error("identical contents have different sums")
	}
	if tree["a"].Sum == tree["c"].Sum {
		t.Error("different contents have the same sum")
	}
}
