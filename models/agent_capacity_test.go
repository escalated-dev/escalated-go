package models

import "testing"

func TestAgentCapacityHasCapacity(t *testing.T) {
	if !(AgentCapacity{CurrentCount: 3, MaxConcurrent: 10}).HasCapacity() {
		t.Error("3/10 should have capacity")
	}
	if (AgentCapacity{CurrentCount: 10, MaxConcurrent: 10}).HasCapacity() {
		t.Error("10/10 should be at capacity")
	}
}

func TestAgentCapacityLoadPercentage(t *testing.T) {
	cases := []struct {
		current, max int
		want         float64
	}{
		{5, 10, 50.0},
		{1, 3, 33.3},
		{0, 10, 0.0},
		{5, 0, 100.0}, // uncapped
	}
	for _, c := range cases {
		got := AgentCapacity{CurrentCount: c.current, MaxConcurrent: c.max}.LoadPercentage()
		if got != c.want {
			t.Errorf("LoadPercentage(%d/%d) = %v, want %v", c.current, c.max, got, c.want)
		}
	}
}
