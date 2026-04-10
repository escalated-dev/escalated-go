package services

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// TwoFactorStore defines the persistence interface for two-factor auth.
type TwoFactorStore interface {
	CreateTwoFactor(tf *models.TwoFactor) error
	FindTwoFactorByUser(userID int64) (*models.TwoFactor, error)
	UpdateTwoFactor(tf *models.TwoFactor) error
}

// TwoFactorService manages two-factor authentication.
type TwoFactorService struct {
	store TwoFactorStore
}

// NewTwoFactorService creates a new TwoFactorService.
func NewTwoFactorService(store TwoFactorStore) *TwoFactorService {
	return &TwoFactorService{store: store}
}

// Enable creates a new 2FA configuration for a user.
func (s *TwoFactorService) Enable(userID int64, method string) (*models.TwoFactor, error) {
	if method == "" {
		method = "totp"
	}
	secret := generateSecret(32)
	codes := generateRecoveryCodes(8)
	tf := &models.TwoFactor{
		UserID:        userID,
		Method:        method,
		Secret:        &secret,
		RecoveryCodes: codes,
		IsEnabled:     true,
	}
	err := s.store.CreateTwoFactor(tf)
	return tf, err
}

// FindByUser returns the active 2FA config for a user.
func (s *TwoFactorService) FindByUser(userID int64) (*models.TwoFactor, error) {
	return s.store.FindTwoFactorByUser(userID)
}

// VerifyRecoveryCode validates and consumes a recovery code.
func (s *TwoFactorService) VerifyRecoveryCode(tf *models.TwoFactor, code string) (bool, error) {
	if tf.UseRecoveryCode(code) {
		err := s.store.UpdateTwoFactor(tf)
		return true, err
	}
	return false, nil
}

// Disable turns off 2FA for a user.
func (s *TwoFactorService) Disable(tf *models.TwoFactor) error {
	tf.IsEnabled = false
	tf.Secret = nil
	tf.RecoveryCodes = nil
	return s.store.UpdateTwoFactor(tf)
}

// RegenerateRecoveryCodes generates new recovery codes.
func (s *TwoFactorService) RegenerateRecoveryCodes(tf *models.TwoFactor) ([]string, error) {
	codes := generateRecoveryCodes(8)
	tf.RecoveryCodes = codes
	err := s.store.UpdateTwoFactor(tf)
	return codes, err
}

func generateSecret(length int) string {
	chars := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	b := make([]byte, length)
	_, _ = rand.Read(b)
	var sb strings.Builder
	for i := 0; i < length; i++ {
		sb.WriteByte(chars[int(b[i])%len(chars)])
	}
	return sb.String()
}

func generateRecoveryCodes(count int) []string {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		a := make([]byte, 4)
		b := make([]byte, 4)
		_, _ = rand.Read(a)
		_, _ = rand.Read(b)
		codes[i] = hex.EncodeToString(a) + "-" + hex.EncodeToString(b)
	}
	return codes
}
