package services

import (
	"testing"
)

func TestExtractMentionsSingle(t *testing.T) {
	result := ExtractMentions("Hello @john please review")
	if len(result) != 1 || result[0] != "john" {
		t.Errorf("expected [john], got %v", result)
	}
}

func TestExtractMentionsMultiple(t *testing.T) {
	result := ExtractMentions("@alice and @bob please check")
	if len(result) != 2 {
		t.Errorf("expected 2 mentions, got %d", len(result))
	}
}

func TestExtractMentionsDotted(t *testing.T) {
	result := ExtractMentions("cc @john.doe")
	if len(result) != 1 || result[0] != "john.doe" {
		t.Errorf("expected [john.doe], got %v", result)
	}
}

func TestExtractMentionsDedup(t *testing.T) {
	result := ExtractMentions("@alice said @alice should review")
	if len(result) != 1 {
		t.Errorf("expected 1 unique mention, got %d", len(result))
	}
}

func TestExtractMentionsEmpty(t *testing.T) {
	result := ExtractMentions("")
	if result != nil {
		t.Errorf("expected nil for empty, got %v", result)
	}
}

func TestExtractMentionsNoMentions(t *testing.T) {
	result := ExtractMentions("No mentions here")
	if result != nil {
		t.Errorf("expected nil for no mentions, got %v", result)
	}
}

func TestExtractUsernameFromEmail(t *testing.T) {
	result := ExtractUsernameFromEmail("john@example.com")
	if result != "john" {
		t.Errorf("expected john, got %s", result)
	}
}
