package models

import "testing"

func TestFieldTypesContainsExpectedTypes(t *testing.T) {
	expected := []string{"text", "textarea", "number", "select", "multi_select", "checkbox", "date", "url"}
	if len(FieldTypes) != len(expected) {
		t.Errorf("expected %d field types, got %d", len(expected), len(FieldTypes))
	}
	for _, e := range expected {
		found := false
		for _, ft := range FieldTypes {
			if ft == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected field type %q not found", e)
		}
	}
}
