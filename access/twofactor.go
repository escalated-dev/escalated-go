package access

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// TOTPPeriod and TOTPSkew are RFC 6238 as the ADR fixes them: 30 s steps,
// and one step of clock drift accepted either side of now.
const (
	TOTPPeriod = 30
	TOTPSkew   = 1
)

// Enrolment is a user's two-factor enrolment.
type Enrolment struct {
	ID            int64
	UserID        models.UserID
	Secret        string
	RecoveryCodes []string
	Confirmed     bool
	LastUsedStep  *int64
}

// TwoFactor stores enrolments in the two_factors table, with the TOTP
// secret and recovery codes sealed (AES-256-GCM) under a key derived from
// the configured secret, and checks codes through pquerna/otp.
type TwoFactor struct {
	DB     *sql.DB
	Prefix string
	// Key seals secrets and recovery codes. Set by Escalated from
	// Config.Access.TwoFactorSecret.
	Key []byte
	// Issuer names the account in authenticator apps.
	Issuer string
	// Now is the clock (tests). Nil is time.Now.
	Now func() time.Time
}

func (t *TwoFactor) table() string { return t.Prefix + "two_factors" }

func (t *TwoFactor) q(query string) string { return sqldialect.Rebind(t.DB, query) }

func (t *TwoFactor) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// Find returns the user's enrolment (confirmed or pending), or nil.
func (t *TwoFactor) Find(ctx context.Context, userID models.UserID) (*Enrolment, error) {
	if userID.Empty() {
		return nil, nil
	}
	var e Enrolment
	var secret, codes sql.NullString
	var verified sql.NullTime
	var enabled bool
	var step sql.NullInt64
	err := t.DB.QueryRowContext(ctx, t.q("SELECT id, user_id, secret, recovery_codes, is_enabled, verified_at, last_used_step FROM "+t.table()+
		" WHERE user_id = ? ORDER BY id DESC LIMIT 1"), string(userID)).
		Scan(&e.ID, &e.UserID, &secret, &codes, &enabled, &verified, &step)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Confirmed = enabled && verified.Valid
	if step.Valid {
		v := step.Int64
		e.LastUsedStep = &v
	}
	if secret.Valid && secret.String != "" {
		plain, err := t.open(secret.String)
		if err != nil {
			return nil, fmt.Errorf("escalated: two-factor secret of user %s cannot be opened: %w", userID, err)
		}
		e.Secret = string(plain)
	}
	if codes.Valid && codes.String != "" {
		plain, err := t.open(codes.String)
		if err != nil {
			return nil, fmt.Errorf("escalated: recovery codes of user %s cannot be opened: %w", userID, err)
		}
		if err := json.Unmarshal(plain, &e.RecoveryCodes); err != nil {
			return nil, err
		}
	}
	return &e, nil
}

// Confirmed returns the user's confirmed enrolment, or nil.
func (t *TwoFactor) Confirmed(ctx context.Context, userID models.UserID) (*Enrolment, error) {
	e, err := t.Find(ctx, userID)
	if err != nil || e == nil || !e.Confirmed {
		return nil, err
	}
	return e, nil
}

// Setup replaces any pending enrolment with a new secret and eight recovery
// codes, and returns the otpauth:// URI and the codes. accountName labels
// the account in the app (the user's email, else their id).
func (t *TwoFactor) Setup(ctx context.Context, userID models.UserID, accountName string) (uri string, codes []string, err error) {
	issuer := t.Issuer
	if issuer == "" {
		issuer = "Escalated"
	}
	if accountName == "" {
		accountName = string(userID)
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: accountName, Period: TOTPPeriod, SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return "", nil, err
	}
	codes, err = RecoveryCodes()
	if err != nil {
		return "", nil, err
	}
	sealedSecret, err := t.seal([]byte(key.Secret()))
	if err != nil {
		return "", nil, err
	}
	rawCodes, _ := json.Marshal(codes)
	sealedCodes, err := t.seal(rawCodes)
	if err != nil {
		return "", nil, err
	}
	tx, err := t.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, t.q("DELETE FROM "+t.table()+" WHERE user_id = ? AND (is_enabled = ? OR verified_at IS NULL)"), string(userID), false); err != nil {
		return "", nil, err
	}
	now := t.now().UTC()
	if _, err := tx.ExecContext(ctx, t.q("INSERT INTO "+t.table()+" (user_id, method, secret, recovery_codes, is_enabled, created_at, updated_at) VALUES (?, 'totp', ?, ?, ?, ?, ?)"),
		string(userID), sealedSecret, sealedCodes, false, now, now); err != nil {
		return "", nil, err
	}
	return key.URL(), codes, tx.Commit()
}

// Confirm checks a code against the pending enrolment and confirms it.
func (t *TwoFactor) Confirm(ctx context.Context, e *Enrolment, code string) (bool, error) {
	ok, err := t.VerifyAndConsume(ctx, e, code)
	if err != nil || !ok {
		return ok, err
	}
	now := t.now().UTC()
	_, err = t.DB.ExecContext(ctx, t.q("UPDATE "+t.table()+" SET is_enabled = ?, verified_at = ?, updated_at = ? WHERE id = ?"), true, now, now, e.ID)
	return err == nil, err
}

// VerifyAndConsume checks a six-digit code for the enrolment and records its
// time step, refusing that step or any earlier one afterwards, so a code
// works once. The step is claimed with a conditional update, so two requests
// racing with one code cannot both pass.
func (t *TwoFactor) VerifyAndConsume(ctx context.Context, e *Enrolment, code string) (bool, error) {
	code = strings.Join(strings.Fields(code), "")
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" || e == nil || e.Secret == "" {
		return false, nil
	}
	current := int64(math.Floor(float64(t.now().Unix()) / TOTPPeriod))
	for step := current - TOTPSkew; step <= current+TOTPSkew; step++ {
		if e.LastUsedStep != nil && step <= *e.LastUsedStep {
			continue
		}
		valid, err := hotp.ValidateCustom(code, uint64(step), e.Secret, hotp.ValidateOpts{Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err != nil {
			return false, err
		}
		if !valid {
			continue
		}
		res, err := t.DB.ExecContext(ctx, t.q("UPDATE "+t.table()+" SET last_used_step = ? WHERE id = ? AND (last_used_step IS NULL OR last_used_step < ?)"), step, e.ID, step)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		if n == 1 {
			e.LastUsedStep = &step
			return true, nil
		}
		return false, nil
	}
	return false, nil
}

// UseRecoveryCode signs in with a recovery code: true when it matches one,
// which is then removed.
func (t *TwoFactor) UseRecoveryCode(ctx context.Context, e *Enrolment, code string) (bool, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || e == nil {
		return false, nil
	}
	for i, candidate := range e.RecoveryCodes {
		if subtle.ConstantTimeCompare([]byte(strings.ToUpper(candidate)), []byte(code)) != 1 {
			continue
		}
		left := append(append([]string{}, e.RecoveryCodes[:i]...), e.RecoveryCodes[i+1:]...)
		raw, _ := json.Marshal(left)
		sealed, err := t.seal(raw)
		if err != nil {
			return false, err
		}
		if _, err := t.DB.ExecContext(ctx, t.q("UPDATE "+t.table()+" SET recovery_codes = ?, updated_at = ? WHERE id = ?"), sealed, t.now().UTC(), e.ID); err != nil {
			return false, err
		}
		e.RecoveryCodes = left
		return true, nil
	}
	return false, nil
}

// Disable removes the user's enrolment. It is also how an admin clears a
// user's enrolment after a lost device (escalated:two-factor:reset).
func (t *TwoFactor) Disable(ctx context.Context, userID models.UserID) error {
	_, err := t.DB.ExecContext(ctx, t.q("DELETE FROM "+t.table()+" WHERE user_id = ?"), string(userID))
	return err
}

// RecoveryCodes returns eight random single-use codes, XXXXXXXX-XXXXXXXX.
func RecoveryCodes() ([]string, error) {
	codes := make([]string, 8)
	for i := range codes {
		b := make([]byte, 8)
		if _, err := io.ReadFull(rand.Reader, b); err != nil {
			return nil, err
		}
		codes[i] = strings.ToUpper(hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:]))
	}
	return codes, nil
}

func (t *TwoFactor) aead() (cipher.AEAD, error) {
	if len(t.Key) == 0 {
		return nil, errors.New("escalated: no two-factor key")
	}
	key := sha256.Sum256(append([]byte("escalated two-factor seal\x00"), t.Key...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (t *TwoFactor) seal(plain []byte) (string, error) {
	aead, err := t.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, nil)), nil
}

func (t *TwoFactor) open(sealed string) ([]byte, error) {
	aead, err := t.aead()
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < aead.NonceSize() {
		return nil, errors.New("not a sealed value")
	}
	return aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
}

// passMark is the value of the challenge cookie: the user id, bound to the
// host's session id by an HMAC, so a new login (a new session id) or another
// user is challenged again.
func passMark(key []byte, userID models.UserID, sessionID string) string {
	mac := hmac.New(sha256.New, append([]byte("escalated two-factor pass\x00"), key...))
	mac.Write([]byte(string(userID) + "\x00" + sessionID))
	return base64.RawURLEncoding.EncodeToString([]byte(userID)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validMark(key []byte, mark string, userID models.UserID, sessionID string) bool {
	if mark == "" || sessionID == "" || userID.Empty() {
		return false
	}
	return hmac.Equal([]byte(mark), []byte(passMark(key, userID, sessionID)))
}

// attempts is the verify route's limiter: limit attempts per key a minute.
type attempts struct {
	mu    sync.Mutex
	limit int
	hits  map[string][]time.Time
	now   func() time.Time
}

func (a *attempts) allow(key string) (bool, time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if a.hits == nil {
		a.hits = map[string][]time.Time{}
	}
	recent := a.hits[key][:0]
	for _, at := range a.hits[key] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	if len(recent) >= a.limit {
		a.hits[key] = recent
		return false, time.Minute - now.Sub(recent[0])
	}
	a.hits[key] = append(recent, now)
	return true, 0
}
