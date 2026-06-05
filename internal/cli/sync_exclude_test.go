package cli

import (
	"testing"
)

// names extracts the (deduplicated, order-preserving) dependent resource names
// from a slice of defs. Comments register twice (per task and per project), so
// dedup keeps assertions about "is comments selected" readable.
func names(defs []dependentResourceDef) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range defs {
		if !seen[d.Name] {
			seen[d.Name] = true
			out = append(out, d.Name)
		}
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestSelectedDependents_ExcludeDropsDependentByName(t *testing.T) {
	defs := dependentResourceDefs()

	// No filter, no exclude: every dependent runs (comments included).
	all := names(selectedDependents(defs, nil, nil))
	if !contains(all, "comments") {
		t.Fatalf("expected comments selected by default, got %v", all)
	}

	// --exclude comments: comments dropped even though its parents (tasks,
	// projects) are in scope; siblings like collaborators still run.
	excl := map[string]bool{"comments": true}
	got := names(selectedDependents(defs, nil, excl))
	if contains(got, "comments") {
		t.Fatalf("comments should be excluded, got %v", got)
	}
	if !contains(got, "collaborators") {
		t.Fatalf("collaborators should still run when only comments is excluded, got %v", got)
	}
}

func TestSelectedDependents_ExcludeWinsOverParentAllow(t *testing.T) {
	defs := dependentResourceDefs()

	// --resources tasks names a parent that cascades to comments, but
	// --exclude comments must still suppress it.
	allow := map[string]bool{"tasks": true}
	excl := map[string]bool{"comments": true}
	got := names(selectedDependents(defs, allow, excl))
	if contains(got, "comments") {
		t.Fatalf("exclude must win over parent cascade, got %v", got)
	}
}
