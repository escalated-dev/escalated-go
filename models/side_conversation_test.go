package models

import "testing"

func TestValidSideConversationChannel(t *testing.T) {
	for _, c := range []string{SideConversationChannelInternal, SideConversationChannelEmail} {
		if !ValidSideConversationChannel(c) {
			t.Errorf("ValidSideConversationChannel(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"", "sms", "Internal", "phone"} {
		if ValidSideConversationChannel(c) {
			t.Errorf("ValidSideConversationChannel(%q) = true, want false", c)
		}
	}
}
