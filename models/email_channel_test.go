package models

import "testing"

func TestEmailChannelFormattedSender(t *testing.T) {
	tests := []struct {
		name     string
		channel  EmailChannel
		expected string
	}{
		{
			name:     "with display name",
			channel:  EmailChannel{EmailAddress: "support@example.com", DisplayName: strPtr("Support Team")},
			expected: "Support Team <support@example.com>",
		},
		{
			name:     "without display name",
			channel:  EmailChannel{EmailAddress: "support@example.com"},
			expected: "support@example.com",
		},
		{
			name:     "with empty display name",
			channel:  EmailChannel{EmailAddress: "support@example.com", DisplayName: strPtr("")},
			expected: "support@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.channel.FormattedSender()
			if result != tt.expected {
				t.Errorf("FormattedSender() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
