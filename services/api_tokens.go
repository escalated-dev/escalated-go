package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// APITokenService stores REST API bearer tokens (escalated-laravel ApiToken).
//
// A token is 64 hex characters of crypto/rand output. Only its SHA-256 digest
// is stored, so a leaked database does not leak usable tokens; the plain text
// is returned once, by Create.
type APITokenService struct {
	DB     *sql.DB
	Prefix string
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// LastUsedInterval is how stale last_used_at must be before a request
// rewrites it, so an active token does not cost a write per request.
const LastUsedInterval = 5 * time.Minute

func (s *APITokenService) table() string { return s.Prefix + "api_tokens" }

func (s *APITokenService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// HashAPIToken is the digest stored for a plain-text token.
func HashAPIToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

const apiTokenColumns = `id, user_id, name, token, abilities, last_used_at, last_used_ip, expires_at, created_at, updated_at`

// Create issues a token for userID and returns it with its plain text.
func (s *APITokenService) Create(ctx context.Context, userID models.UserID, name string, abilities []string, expiresAt *time.Time) (*models.APIToken, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	plain := hex.EncodeToString(raw)
	if len(abilities) == 0 {
		abilities = []string{models.APITokenAbilityAll}
	}
	encoded, err := json.Marshal(abilities)
	if err != nil {
		return nil, "", err
	}
	now := s.now().UTC()
	res, err := sqldialect.ExecInsertContext(ctx, s.DB,
		`INSERT INTO `+s.table()+` (user_id, name, token, abilities, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(userID), name, HashAPIToken(plain), string(encoded), utcPtr(expiresAt), now, now,
	)
	if err != nil {
		return nil, "", err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, "", err
	}
	token, err := s.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return token, plain, nil
}

// List returns every token, newest first.
func (s *APITokenService) List(ctx context.Context) ([]*models.APIToken, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+apiTokenColumns+` FROM `+s.table()+` ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.APIToken{}
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get returns the token with id, or nil.
func (s *APITokenService) Get(ctx context.Context, id int64) (*models.APIToken, error) {
	return s.one(ctx, `id = ?`, id)
}

// FindByPlainText returns the token whose digest matches plain, or nil.
func (s *APITokenService) FindByPlainText(ctx context.Context, plain string) (*models.APIToken, error) {
	if plain == "" {
		return nil, nil
	}
	return s.one(ctx, `token = ?`, HashAPIToken(plain))
}

func (s *APITokenService) one(ctx context.Context, where string, arg any) (*models.APIToken, error) {
	row := s.DB.QueryRowContext(ctx, sqldialect.Rebind(s.DB, `SELECT `+apiTokenColumns+` FROM `+s.table()+` WHERE `+where), arg)
	t, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// Update renames the token and/or replaces its abilities; nil leaves a field
// as it is. Reports whether the token exists.
func (s *APITokenService) Update(ctx context.Context, id int64, name *string, abilities []string) (bool, error) {
	token, err := s.Get(ctx, id)
	if err != nil || token == nil {
		return false, err
	}
	if name != nil {
		token.Name = *name
	}
	if abilities != nil {
		token.Abilities = abilities
	}
	encoded, err := json.Marshal(token.Abilities)
	if err != nil {
		return false, err
	}
	_, err = s.DB.ExecContext(ctx, sqldialect.Rebind(s.DB, `UPDATE `+s.table()+` SET name = ?, abilities = ?, updated_at = ? WHERE id = ?`),
		token.Name, string(encoded), s.now().UTC(), id)
	return err == nil, err
}

// Delete revokes the token. Reports whether it existed.
func (s *APITokenService) Delete(ctx context.Context, id int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, sqldialect.Rebind(s.DB, `DELETE FROM `+s.table()+` WHERE id = ?`), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Touch records a use of the token from ip, unless it was recorded within
// LastUsedInterval.
func (s *APITokenService) Touch(ctx context.Context, token *models.APIToken, ip string) error {
	now := s.now().UTC()
	if token.LastUsedAt != nil && now.Sub(*token.LastUsedAt) < LastUsedInterval {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, sqldialect.Rebind(s.DB, `UPDATE `+s.table()+` SET last_used_at = ?, last_used_ip = ? WHERE id = ?`), now, ip, token.ID)
	return err
}

func scanAPIToken(row rowScanner) (*models.APIToken, error) {
	var (
		t         models.APIToken
		abilities sql.NullString
		lastUsed  sql.NullTime
		lastIP    sql.NullString
		expires   sql.NullTime
	)
	if err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &abilities, &lastUsed, &lastIP, &expires, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Abilities = []string{}
	if abilities.Valid && abilities.String != "" {
		if err := json.Unmarshal([]byte(abilities.String), &t.Abilities); err != nil {
			return nil, err
		}
	}
	if lastUsed.Valid {
		v := lastUsed.Time
		t.LastUsedAt = &v
	}
	if lastIP.Valid {
		v := lastIP.String
		t.LastUsedIP = &v
	}
	if expires.Valid {
		v := expires.Time
		t.ExpiresAt = &v
	}
	return &t, nil
}

func utcPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
