package models

import "testing"

func TestValidLinkType(t *testing.T) {
	for _, lt := range []string{LinkTypeProblemIncident, LinkTypeParentChild, LinkTypeRelated} {
		if !ValidLinkType(lt) {
			t.Errorf("ValidLinkType(%q) = false, want true", lt)
		}
	}
	for _, lt := range []string{"", "bogus", "duplicate", "PARENT_CHILD"} {
		if ValidLinkType(lt) {
			t.Errorf("ValidLinkType(%q) = true, want false", lt)
		}
	}
}
