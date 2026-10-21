package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/middleware"
)

// Verified guest access, ported from escalated-laravel GuestEmailVerification
// and GuestAccess (docs/guest-access.md):
//
//   - A guest proves they read a mailbox with an eight-digit code sent to it.
//     A code is bound to the normalised email and a purpose (ticket, chat or
//     lookup), expires after ten minutes, allows five guesses and is consumed
//     once. Only an HMAC digest of it is stored.
//   - Code delivery is budgeted per mailbox and client IP (3 an hour) and per
//     mailbox across clients (10 an hour).
//   - A verified ticket gets an expiring grant: sealed (AES-256-GCM) claims
//     naming the ticket, the verified email and a nonce. The ticket stores only
//     a SHA-256 of the nonce, so issuing a new grant invalidates older ones.

// Guest verification purposes.
const (
	GuestPurposeTicket = "ticket"
	GuestPurposeChat   = "chat"
	GuestPurposeLookup = "lookup"
)

const (
	guestCodeTTL         = 10 * time.Minute
	guestCodeMaxAttempts = 5
	guestLookupLimit     = 20
	defaultGuestGrantTTL = 24 * time.Hour
	minGuestGrantTTL     = 5 * time.Minute
	maxGuestGrantTTL     = 7 * 24 * time.Hour
)

// GuestCodeExpiresIn is the code lifetime in seconds, as /verification reports it.
const GuestCodeExpiresIn = int(guestCodeTTL / time.Second)

var (
	// ErrGuestEmailInvalid is returned for a missing or malformed email or purpose.
	ErrGuestEmailInvalid = errors.New("enter a valid email address")
	// ErrGuestCodeInvalid is returned when a code is wrong, expired, used up or
	// was issued for another email or purpose.
	ErrGuestCodeInvalid = errors.New("this code is invalid, expired or already used; request a new code")
	// ErrGuestGrantInvalid is returned when a guest token does not open a ticket.
	ErrGuestGrantInvalid = errors.New("guest access is invalid or has expired")
)

// GuestCodeBudgetError is returned when too many codes were requested for a
// mailbox. RetryAfter is how long until another may be sent.
type GuestCodeBudgetError struct {
	RetryAfter time.Duration
}

func (e *GuestCodeBudgetError) Error() string {
	return "please wait before requesting another code"
}

// GuestAccessConfig configures verified guest access.
type GuestAccessConfig struct {
	// Secret keys the code digests and seals the grants. Use a long random
	// value and the same one on every instance; changing it invalidates
	// outstanding codes and grants. When empty, a random key is generated at
	// startup (grants then die with the process and do not work across
	// instances), and a warning is logged.
	Secret string

	// TTL is how long a guest grant lasts. Zero means 24 hours; it is clamped
	// to 5 minutes through 7 days.
	TTL time.Duration

	// CodesPerClientPerHour caps codes sent to one mailbox from one client IP
	// an hour. Zero means 3.
	CodesPerClientPerHour int

	// CodesPerMailboxPerHour caps codes sent to one mailbox from all clients
	// an hour. Zero means 10 (never less than CodesPerClientPerHour).
	CodesPerMailboxPerHour int

	// RejectLegacyTokens stops the permanent guest_token issued before
	// verified access from opening a ticket. Off by default for the
	// transition: such a token keeps working until a grant is issued for its
	// ticket (which clears it) or this is switched on. Laravel no longer
	// accepts them at all.
	RejectLegacyTokens bool
}

// GuestAccess issues and checks guest email codes and ticket grants.
type GuestAccess struct {
	DB     *sql.DB
	Prefix string
	Config GuestAccessConfig
	// Mailer sends the codes. Nil means LogMailSender.
	Mailer MailSender
	// From is the From address on code emails (empty leaves it to the mailer).
	From string
	// Budget counts code requests. Nil means a per-process memory store.
	Budget middleware.GuestRateLimitStore
	Logger *log.Logger

	macKey []byte
	encKey []byte
	now    func() time.Time
}

// NewGuestAccess builds the service. prefix is the table prefix.
func NewGuestAccess(db *sql.DB, prefix string, cfg GuestAccessConfig, mailer MailSender, from string, budget middleware.GuestRateLimitStore, logger *log.Logger) *GuestAccess {
	if logger == nil {
		logger = log.Default()
	}
	secret := cfg.Secret
	if secret == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		secret = hex.EncodeToString(b)
		logger.Printf("escalated: GuestAccess.Secret is empty; using a random key. Guest grants will not survive a restart or work across instances.")
	}
	if budget == nil {
		budget = middleware.NewMemoryGuestRateLimitStore()
	}
	if mailer == nil {
		mailer = LogMailSender{Logger: logger}
	}
	mac := sha256.Sum256([]byte("escalated-guest-mac:" + secret))
	enc := sha256.Sum256([]byte("escalated-guest-enc:" + secret))
	return &GuestAccess{
		DB: db, Prefix: prefix, Config: cfg, Mailer: mailer, From: from, Budget: budget, Logger: logger,
		macKey: mac[:], encKey: enc[:], now: time.Now,
	}
}

// NormalizeGuestEmail lower-cases and trims an email address.
func NormalizeGuestEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidGuestEmail reports whether email is a bare, well-formed address.
func ValidGuestEmail(email string) bool {
	email = NormalizeGuestEmail(email)
	if email == "" || len(email) > 255 {
		return false
	}
	a, err := mail.ParseAddress(email)
	return err == nil && a.Address == email && a.Name == ""
}

// ValidGuestPurpose reports whether purpose is ticket, chat or lookup.
func ValidGuestPurpose(purpose string) bool {
	return purpose == GuestPurposeTicket || purpose == GuestPurposeChat || purpose == GuestPurposeLookup
}

func (g *GuestAccess) t(name string) string { return g.Prefix + name }

func (g *GuestAccess) q(query string) string { return sqldialect.Rebind(g.DB, query) }

func (g *GuestAccess) clock() time.Time {
	if g.now != nil {
		return g.now().UTC()
	}
	return time.Now().UTC()
}

func (g *GuestAccess) digest(value string) string {
	m := hmac.New(sha256.New, g.macKey)
	m.Write([]byte("escalated-guest-email:" + value))
	return hex.EncodeToString(m.Sum(nil))
}

func (g *GuestAccess) grantTTL() time.Duration {
	ttl := g.Config.TTL
	if ttl == 0 {
		ttl = defaultGuestGrantTTL
	}
	if ttl < minGuestGrantTTL {
		ttl = minGuestGrantTTL
	}
	if ttl > maxGuestGrantTTL {
		ttl = maxGuestGrantTTL
	}
	return ttl
}

// Challenge emails a one-time code for purpose to email and returns the
// verification id the guest sends back with the code. clientIP keys the
// per-client budget.
func (g *GuestAccess) Challenge(ctx context.Context, email, purpose, clientIP string) (string, error) {
	email = NormalizeGuestEmail(email)
	if !ValidGuestEmail(email) || !ValidGuestPurpose(purpose) {
		return "", ErrGuestEmailInvalid
	}
	if err := g.spendBudget(ctx, email, clientIP); err != nil {
		return "", err
	}

	now := g.clock()
	// Expired codes are of no use to anyone; clear them as we go.
	_, _ = g.DB.ExecContext(ctx, g.q(`DELETE FROM `+g.t("guest_verifications")+` WHERE expires_at < ?`), now.Add(-24*time.Hour))

	id, err := newUUID()
	if err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%08d", n.Int64())
	if _, err := g.DB.ExecContext(ctx, g.q(`INSERT INTO `+g.t("guest_verifications")+`
		(id, email, purpose, code_hash, attempts, expires_at, created_at) VALUES (?, ?, ?, ?, 0, ?, ?)`),
		id, email, purpose, g.digest(id+":"+code), now.Add(guestCodeTTL), now,
	); err != nil {
		return "", err
	}

	err = g.Mailer.SendMail(ctx, Mail{
		To:      email,
		From:    g.From,
		Subject: "Your support verification code",
		Text: "Your support verification code is: " + code + "\n\n" +
			"This code expires in 10 minutes and can be used once. Enter it in the support form where you requested it.\n\n" +
			"If you did not request this code, you can ignore this email. No ticket access has been granted.\n",
	})
	if err != nil {
		_, _ = g.DB.ExecContext(ctx, g.q(`DELETE FROM `+g.t("guest_verifications")+` WHERE id = ?`), id)
		return "", err
	}
	return id, nil
}

// spendBudget counts a code request against the per-client and per-mailbox
// hourly budgets. The client budget is checked first, so a client over its
// own budget does not use up the mailbox's.
func (g *GuestAccess) spendBudget(ctx context.Context, email, clientIP string) error {
	perClient := g.Config.CodesPerClientPerHour
	if perClient <= 0 {
		perClient = 3
	}
	perMailbox := g.Config.CodesPerMailboxPerHour
	if perMailbox <= 0 {
		perMailbox = 10
	}
	if perMailbox < perClient {
		perMailbox = perClient
	}
	key := "escalated:guest-email:" + g.digest(email)
	for _, b := range []struct {
		key   string
		limit int
	}{
		{key + ":client:" + g.digest(clientIP), perClient},
		{key, perMailbox},
	} {
		count, resetIn, err := g.Budget.Hit(ctx, b.key, time.Hour)
		if err != nil {
			return err
		}
		if count > b.limit {
			if resetIn < time.Second {
				resetIn = time.Second
			}
			return &GuestCodeBudgetError{RetryAfter: resetIn}
		}
	}
	return nil
}

// Consume checks a code and uses it up. A wrong guess still counts against
// the five allowed. Call Release if the write the code authorised fails, so
// the guest can retry with the same code.
func (g *GuestAccess) Consume(ctx context.Context, id, code, email, purpose string) error {
	if id == "" || code == "" || len(code) > 16 || len(id) > 64 {
		return ErrGuestCodeInvalid
	}
	now := g.clock()
	res, err := g.DB.ExecContext(ctx, g.q(`UPDATE `+g.t("guest_verifications")+`
		SET attempts = attempts + 1 WHERE id = ? AND used_at IS NULL AND expires_at > ? AND attempts < ?`),
		id, now, guestCodeMaxAttempts)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrGuestCodeInvalid
	}
	var storedEmail, storedPurpose, hash string
	if err := g.DB.QueryRowContext(ctx, g.q(`SELECT email, purpose, code_hash FROM `+g.t("guest_verifications")+` WHERE id = ?`), id).
		Scan(&storedEmail, &storedPurpose, &hash); err != nil {
		return ErrGuestCodeInvalid
	}
	if !hmac.Equal([]byte(hash), []byte(g.digest(id+":"+code))) ||
		storedEmail != NormalizeGuestEmail(email) || storedPurpose != purpose {
		return ErrGuestCodeInvalid
	}
	res, err = g.DB.ExecContext(ctx, g.q(`UPDATE `+g.t("guest_verifications")+` SET used_at = ? WHERE id = ? AND used_at IS NULL`), now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrGuestCodeInvalid
	}
	return nil
}

// Release hands back a consumed code after the write it authorised failed.
func (g *GuestAccess) Release(ctx context.Context, id string) {
	_, _ = g.DB.ExecContext(ctx, g.q(`UPDATE `+g.t("guest_verifications")+` SET used_at = NULL WHERE id = ?`), id)
}

type guestClaims struct {
	Version int    `json:"v"`
	Ticket  string `json:"t"`
	Purpose string `json:"p"`
	Email   string `json:"e"`
	Nonce   string `json:"n"`
	Expires int64  `json:"x"`
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Issue marks the ticket's guest email as verified and returns a new grant
// for it, invalidating earlier grants and the ticket's legacy guest token.
// verifiedEmail must be the ticket's guest email; the caller must already
// have consumed a code proving it.
func (g *GuestAccess) Issue(ctx context.Context, ticketID int64, verifiedEmail string) (string, time.Time, error) {
	email := NormalizeGuestEmail(verifiedEmail)
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", time.Time{}, err
	}
	nonce := hex.EncodeToString(nonceBytes)
	now := g.clock().Truncate(time.Second)
	expires := now.Add(g.grantTTL())

	res, err := g.DB.ExecContext(ctx, g.q(`UPDATE `+g.t("tickets")+`
		SET guest_verified_email = ?, guest_email_verified_at = ?, guest_access_hash = ?, guest_access_expires_at = ?, guest_token = NULL
		WHERE id = ? AND LOWER(guest_email) = ?`),
		email, now, sha256Hex(nonce), expires, ticketID, email)
	if err != nil {
		return "", time.Time{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 || email == "" {
		return "", time.Time{}, fmt.Errorf("ticket %d has no guest email %q to verify", ticketID, email)
	}

	claims, _ := json.Marshal(guestClaims{
		Version: 1, Ticket: strconv.FormatInt(ticketID, 10), Purpose: GuestPurposeTicket,
		Email: sha256Hex(email), Nonce: nonce, Expires: expires.Unix(),
	})
	token, err := g.seal(claims)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

var guestTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Resolve returns the ticket id a grant opens, or ErrGuestGrantInvalid.
func (g *GuestAccess) Resolve(ctx context.Context, token string) (int64, error) {
	if len(token) > 4096 || !guestTokenPattern.MatchString(token) {
		return 0, ErrGuestGrantInvalid
	}
	raw, err := g.open(token)
	if err != nil {
		return 0, ErrGuestGrantInvalid
	}
	var c guestClaims
	if json.Unmarshal(raw, &c) != nil || c.Version != 1 || c.Purpose != GuestPurposeTicket || len(c.Nonce) != 64 {
		return 0, ErrGuestGrantInvalid
	}
	now := g.clock()
	if c.Expires <= now.Unix() {
		return 0, ErrGuestGrantInvalid
	}
	id, err := strconv.ParseInt(c.Ticket, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrGuestGrantInvalid
	}

	var hash, verified, guestEmail sql.NullString
	var expires, verifiedAt sql.NullTime
	err = g.DB.QueryRowContext(ctx, g.q(`SELECT guest_access_hash, guest_access_expires_at, guest_verified_email, guest_email_verified_at, guest_email
		FROM `+g.t("tickets")+` WHERE id = ?`), id).Scan(&hash, &expires, &verified, &verifiedAt, &guestEmail)
	if err != nil {
		return 0, ErrGuestGrantInvalid
	}
	if !hash.Valid || !expires.Valid || !verified.Valid || !verifiedAt.Valid || !guestEmail.Valid ||
		NormalizeGuestEmail(guestEmail.String) != verified.String ||
		!expires.Time.After(now) || expires.Time.Unix() != c.Expires ||
		!hmac.Equal([]byte(hash.String), []byte(sha256Hex(c.Nonce))) ||
		!hmac.Equal([]byte(c.Email), []byte(sha256Hex(verified.String))) {
		return 0, ErrGuestGrantInvalid
	}
	return id, nil
}

// ResolveTicketID opens a ticket from a guest grant or, unless
// RejectLegacyTokens is set, from the ticket's legacy permanent guest token.
func (g *GuestAccess) ResolveTicketID(ctx context.Context, token string) (int64, error) {
	if id, err := g.Resolve(ctx, token); err == nil {
		return id, nil
	}
	if g.Config.RejectLegacyTokens || token == "" || len(token) > 255 {
		return 0, ErrGuestGrantInvalid
	}
	var id int64
	if err := g.DB.QueryRowContext(ctx, g.q(`SELECT id FROM `+g.t("tickets")+` WHERE guest_token = ?`), token).Scan(&id); err != nil {
		return 0, ErrGuestGrantInvalid
	}
	return id, nil
}

// LegacyTokensAccepted reports whether legacy guest tokens still open tickets.
func (g *GuestAccess) LegacyTokensAccepted() bool { return !g.Config.RejectLegacyTokens }

// Revoke invalidates every grant and the legacy token for a ticket. Call it
// when host code changes a ticket's guest email.
func (g *GuestAccess) Revoke(ctx context.Context, ticketID int64) error {
	_, err := g.DB.ExecContext(ctx, g.q(`UPDATE `+g.t("tickets")+`
		SET guest_access_hash = NULL, guest_access_expires_at = NULL, guest_token = NULL WHERE id = ?`), ticketID)
	return err
}

// GuestLookupMatch is one ticket found by a verified lookup.
type GuestLookupMatch struct {
	Reference        string `json:"reference"`
	Subject          string `json:"subject"`
	GuestAccessToken string `json:"guest_access_token"`
	ExpiresAt        string `json:"expires_at"`
}

// Lookup returns up to 20 tickets with this reference whose guest email is
// email, issuing a fresh grant for each. The caller must already have
// consumed a lookup code for email.
func (g *GuestAccess) Lookup(ctx context.Context, email, reference string) ([]GuestLookupMatch, error) {
	email = NormalizeGuestEmail(email)
	rows, err := g.DB.QueryContext(ctx, g.q(`SELECT id, reference, subject FROM `+g.t("tickets")+`
		WHERE reference = ? AND LOWER(guest_email) = ? ORDER BY created_at DESC, id DESC LIMIT `+strconv.Itoa(guestLookupLimit)),
		strings.TrimSpace(reference), email)
	if err != nil {
		return nil, err
	}
	type hit struct {
		id         int64
		ref, title string
	}
	var hits []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.ref, &h.title); err != nil {
			rows.Close()
			return nil, err
		}
		hits = append(hits, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []GuestLookupMatch{}
	for _, h := range hits {
		token, expires, err := g.Issue(ctx, h.id, email)
		if err != nil {
			return nil, err
		}
		out = append(out, GuestLookupMatch{Reference: h.ref, Subject: h.title, GuestAccessToken: token, ExpiresAt: expires.Format(time.RFC3339)})
	}
	return out, nil
}

func (g *GuestAccess) seal(plain []byte) (string, error) {
	block, err := aes.NewCipher(g.encKey)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, plain, []byte("escalated-guest-grant"))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (g *GuestAccess) open(token string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(g.encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < aead.NonceSize() {
		return nil, errors.New("short token")
	}
	return aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte("escalated-guest-grant"))
}

func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
