package models

import (
	"encoding/json"
	"testing"
)

func TestSavedView_DecodeFilters(t *testing.T) {
	tests := []struct {
		name       string
		filters    json.RawMessage
		wantErr    bool
		wantSearch string
	}{
		{
			name:       "valid filters",
			filters:    json.RawMessage(`{"status":0,"search":"billing","unassigned":true}`),
			wantSearch: "billing",
		},
		{
			name:    "empty filters",
			filters: nil,
		},
		{
			name:    "invalid JSON",
			filters: json.RawMessage(`{invalid`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := &SavedView{Filters: tt.filters}
			f, err := sv.DecodeFilters()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if f.Search != tt.wantSearch {
				t.Errorf("Search = %q, want %q", f.Search, tt.wantSearch)
			}
		})
	}
}

func TestSavedViewFilters_ToTicketFilters(t *testing.T) {
	status := 1
	f := &SavedViewFilters{
		Status:    &status,
		Search:    "test",
		SortBy:    "created_at",
		SortOrder: "desc",
	}

	tf := f.ToTicketFilters(50, 10)

	if tf.Status == nil || *tf.Status != 1 {
		t.Error("expected Status to be 1")
	}
	if tf.Search != "test" {
		t.Errorf("Search = %q, want 'test'", tf.Search)
	}
	if tf.Limit != 50 {
		t.Errorf("Limit = %d, want 50", tf.Limit)
	}
	if tf.Offset != 10 {
		t.Errorf("Offset = %d, want 10", tf.Offset)
	}
	if tf.SortBy != "created_at" {
		t.Errorf("SortBy = %q, want 'created_at'", tf.SortBy)
	}
}
