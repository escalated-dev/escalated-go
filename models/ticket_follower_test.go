package models

import "testing"

func equalUserIDs(a, b []UserID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFollowerRecipientsExcludesActorAndDeduplicates(t *testing.T) {
	got := FollowerRecipients([]UserID{"7", "2", "7", "3"}, "2")
	want := []UserID{"7", "3"}
	if !equalUserIDs(got, want) {
		t.Errorf("FollowerRecipients = %v, want %v", got, want)
	}
}

func TestFollowerRecipientsKeepsAllWhenNoActorExcluded(t *testing.T) {
	got := FollowerRecipients([]UserID{"7", "3", "7"}, "")
	want := []UserID{"7", "3"}
	if !equalUserIDs(got, want) {
		t.Errorf("FollowerRecipients = %v, want %v", got, want)
	}
}
