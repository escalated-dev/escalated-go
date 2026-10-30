package access

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// SSOUsers maps an SSO identity to a host user: matched by email (the
// host's UserIDByEmail, which should compare case-insensitively), created
// only when CreateUsers is on, and with SyncRoles (only while roles &
// permissions is on) the user's Escalated roles replaced by the asserted
// role when it names an existing role. SSO never grants the host's admin or
// agent gate; it only signs the user in.
type SSOUsers struct {
	ByEmail     func(ctx context.Context, email string) (models.UserID, error)
	Create      func(ctx context.Context, email, name string) (models.UserID, error)
	CreateUsers bool
	SyncRoles   bool
	Roles       *Store
}

// Resolve returns the user the identity signs in as.
func (u *SSOUsers) Resolve(ctx context.Context, id Identity) (models.UserID, error) {
	if u.ByEmail == nil {
		return "", errors.New("escalated: SSO needs Config.UserIDByEmail")
	}
	uid, err := u.ByEmail(ctx, strings.ToLower(id.Email))
	if err != nil {
		return "", err
	}
	if uid.Empty() {
		if !u.CreateUsers || u.Create == nil {
			return "", refuse("SSO refused: no user has the email [%s] and creating users is off", id.Email)
		}
		name := id.Name
		if name == "" {
			name = id.Email[:strings.Index(id.Email, "@")]
		}
		uid, err = u.Create(ctx, id.Email, name)
		if err != nil {
			return "", err
		}
		if uid.Empty() {
			return "", errors.New("escalated: SSOOptions.CreateUser returned no user id")
		}
	}
	if u.SyncRoles && u.Roles != nil && id.Role != "" {
		roleID, err := u.Roles.RoleIDBySlug(ctx, id.Role)
		if err != nil {
			return "", err
		}
		if roleID != 0 {
			if err := u.Roles.ReplaceUserRoles(ctx, uid, []int64{roleID}); err != nil {
				return "", err
			}
		}
	}
	return uid, nil
}

// RoleIDBySlug returns the role's id, or 0 when no role has the slug.
func (s *Store) RoleIDBySlug(ctx context.Context, slug string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, s.q("SELECT id FROM "+s.t("roles")+" WHERE slug = ?"), slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
