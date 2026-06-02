package models

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateGuestToken(t *testing.T) {
	const prefix = "GT-"

	token, err := GenerateGuestToken()
	if err != nil {
		t.Fatalf("GenerateGuestToken() error = %v", err)
	}
	if !strings.HasPrefix(token, prefix) {
		t.Fatalf("GenerateGuestToken() = %q, want %q prefix", token, prefix)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, prefix))
	if err != nil {
		t.Fatalf("guest token is not raw URL-safe base64: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded guest token length = %d, want 32", len(decoded))
	}
}

func TestGenerateGuestTokenUnique(t *testing.T) {
	const count = 1000

	seen := make(map[string]struct{}, count)
	for i := 0; i < count; i++ {
		token, err := GenerateGuestToken()
		if err != nil {
			t.Fatalf("GenerateGuestToken() error = %v", err)
		}
		if _, ok := seen[token]; ok {
			t.Fatalf("GenerateGuestToken() returned duplicate token %q", token)
		}
		seen[token] = struct{}{}
	}
}
