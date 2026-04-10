package models

import "testing"

func TestTwoFactorUseRecoveryCode(t *testing.T) {
	tf := &TwoFactor{
		RecoveryCodes: []string{"code-1", "code-2", "code-3"},
	}

	// Use a valid code
	if !tf.UseRecoveryCode("code-2") {
		t.Error("expected UseRecoveryCode to return true for valid code")
	}
	if len(tf.RecoveryCodes) != 2 {
		t.Errorf("expected 2 codes remaining, got %d", len(tf.RecoveryCodes))
	}
	for _, c := range tf.RecoveryCodes {
		if c == "code-2" {
			t.Error("code-2 should have been removed")
		}
	}

	// Use an invalid code
	if tf.UseRecoveryCode("invalid") {
		t.Error("expected UseRecoveryCode to return false for invalid code")
	}
	if len(tf.RecoveryCodes) != 2 {
		t.Errorf("expected 2 codes remaining, got %d", len(tf.RecoveryCodes))
	}
}

func TestTwoFactorUseRecoveryCodeEmpty(t *testing.T) {
	tf := &TwoFactor{RecoveryCodes: nil}
	if tf.UseRecoveryCode("code") {
		t.Error("expected false for nil recovery codes")
	}
}
