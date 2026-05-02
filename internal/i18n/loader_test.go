package i18n

import "testing"

func TestLookupNested(t *testing.T) {
	data := map[string]any{
		"ticket": map[string]any{
			"status": map[string]any{
				"open": "Open",
			},
		},
	}
	got, ok := lookup(data, "ticket.status.open")
	if !ok || got != "Open" {
		t.Fatalf("lookup(ticket.status.open) = %q, %v; want Open, true", got, ok)
	}
	if _, ok := lookup(data, "ticket.missing"); ok {
		t.Fatalf("lookup(ticket.missing) = ok; want missing")
	}
	if _, ok := lookup(data, "ticket.status"); ok {
		// Branch nodes are not strings — should not resolve.
		t.Fatalf("lookup(ticket.status) returned ok on a non-string branch")
	}
}

func TestInterpolate(t *testing.T) {
	got := interpolate("{field} is required", map[string]any{"field": "Email"})
	if got != "Email is required" {
		t.Fatalf("interpolate = %q; want %q", got, "Email is required")
	}

	// Missing param leaves the literal placeholder in place.
	got = interpolate("{field} is required", nil)
	if got != "{field} is required" {
		t.Fatalf("interpolate with nil params = %q; want literal", got)
	}
}

func TestMergeMapOverrides(t *testing.T) {
	dst := map[string]any{
		"ticket": map[string]any{
			"status": map[string]any{
				"open":   "Open",
				"closed": "Closed",
			},
		},
		"keep": "kept",
	}
	src := map[string]any{
		"ticket": map[string]any{
			"status": map[string]any{
				"open": "Aperto", // override only this key
			},
		},
	}
	mergeMap(dst, src)

	got, _ := lookup(dst, "ticket.status.open")
	if got != "Aperto" {
		t.Fatalf("override not applied: got %q", got)
	}
	got, _ = lookup(dst, "ticket.status.closed")
	if got != "Closed" {
		t.Fatalf("sibling key was clobbered: got %q", got)
	}
	if dst["keep"] != "kept" {
		t.Fatalf("unrelated branch was clobbered: %v", dst["keep"])
	}
}

func TestCloneMapIsDeep(t *testing.T) {
	original := map[string]any{
		"a": map[string]any{"b": "c"},
	}
	clone := cloneMap(original)
	clone["a"].(map[string]any)["b"] = "mutated"

	if original["a"].(map[string]any)["b"] != "c" {
		t.Fatalf("cloneMap is shallow: original was mutated")
	}
}
