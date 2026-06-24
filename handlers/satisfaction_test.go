package handlers

import (
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

func TestRatingValid(t *testing.T) {
	for _, r := range []int{1, 2, 3, 4, 5} {
		if !ratingValid(r) {
			t.Errorf("ratingValid(%d) = false, want true", r)
		}
	}
	for _, r := range []int{0, 6, -1, 100} {
		if ratingValid(r) {
			t.Errorf("ratingValid(%d) = true, want false", r)
		}
	}
}

func TestTicketRateable(t *testing.T) {
	if !ticketRateable(models.StatusResolved) {
		t.Error("resolved tickets should be rateable")
	}
	if !ticketRateable(models.StatusClosed) {
		t.Error("closed tickets should be rateable")
	}
	for _, s := range []int{models.StatusOpen, models.StatusInProgress, models.StatusEscalated} {
		if ticketRateable(s) {
			t.Errorf("status %d should not be rateable", s)
		}
	}
}
