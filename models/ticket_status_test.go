package models

import "testing"

func TestStatusCategoriesAndAnchors(t *testing.T) {
	want := []string{"new", "open", "pending", "on_hold", "solved"}
	if len(StatusCategories) != len(want) {
		t.Fatalf("categories %v", StatusCategories)
	}
	for i, c := range want {
		if StatusCategories[i] != c {
			t.Fatalf("categories %v, want %v", StatusCategories, want)
		}
	}

	anchors := map[string]int{
		"new": StatusOpen, "open": StatusOpen, "pending": StatusWaitingOnCustomer,
		"on_hold": StatusInProgress, "solved": StatusResolved,
	}
	for category, status := range anchors {
		if got := CategoryAnchor(category); got != status {
			t.Errorf("anchor(%s) = %s, want %s", category, StatusName[got], StatusName[status])
		}
	}
}

func TestBuiltInStatusCategories(t *testing.T) {
	want := map[int]string{
		StatusOpen: "open", StatusInProgress: "open", StatusWaitingOnCustomer: "pending",
		StatusWaitingOnAgent: "open", StatusEscalated: "open", StatusResolved: "solved",
		StatusClosed: "solved", StatusReopened: "open", StatusSnoozed: "on_hold", StatusLive: "open",
	}
	for status, category := range want {
		if got := BuiltInStatusCategory(status); got != category {
			t.Errorf("category(%s) = %s, want %s", StatusName[status], got, category)
		}
	}
	// Every built-in has a system row description whose slug is its name.
	rows := BuiltInStatuses()
	if len(rows) != len(StatusName) {
		t.Fatalf("%d built-in rows for %d statuses", len(rows), len(StatusName))
	}
	for _, row := range rows {
		if row.Slug != StatusName[row.Status] || row.Label == "" || row.Color == "" {
			t.Errorf("built-in row %+v", row)
		}
	}
}
