package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// Role is an Escalated role (escalated-laravel's Role model).
type Role struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Slug        string       `json:"slug"`
	Description *string      `json:"description"`
	IsSystem    bool         `json:"is_system"`
	UsersCount  int          `json:"users_count"`
	Permissions []Permission `json:"permissions,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// Permission is one permission row.
type Permission struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Group       string  `json:"group"`
	Description *string `json:"description"`
}

// ErrUnknownRole is returned by Assign for a role slug that does not exist.
var ErrUnknownRole = errors.New("escalated: unknown role")

// Store reads and writes roles, permissions and role assignments
// (escalated_roles, escalated_permissions, escalated_role_permission and
// escalated_role_user, with the configured table prefix).
type Store struct {
	DB     *sql.DB
	Prefix string
}

func (s *Store) t(name string) string { return s.Prefix + name }

func (s *Store) q(query string) string { return sqldialect.Rebind(s.DB, query) }

// Seed creates escalated-laravel's permissions and system roles (admin,
// agent, light_agent) and syncs the system roles' permissions. It is
// idempotent: run it after migrating, on every deploy if you like.
func (s *Store) Seed(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	ids := map[string]int64{}
	for _, p := range Permissions {
		var id int64
		err := tx.QueryRowContext(ctx, s.q("SELECT id FROM "+s.t("permissions")+" WHERE slug = ?"), p.Slug).Scan(&id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			err = tx.QueryRowContext(ctx, s.q("INSERT INTO "+s.t("permissions")+` (name, slug, "group", description) VALUES (?, ?, ?, ?) RETURNING id`),
				p.Name, p.Slug, p.Group, p.Description).Scan(&id)
		case err == nil:
			_, err = tx.ExecContext(ctx, s.q("UPDATE "+s.t("permissions")+` SET name = ?, "group" = ?, description = ? WHERE id = ?`),
				p.Name, p.Group, p.Description, id)
		}
		if err != nil {
			return fmt.Errorf("seeding permission %s: %w", p.Slug, err)
		}
		ids[p.Slug] = id
	}

	now := time.Now().UTC()
	for _, role := range SystemRoles {
		var id int64
		err := tx.QueryRowContext(ctx, s.q("SELECT id FROM "+s.t("roles")+" WHERE slug = ?"), role.Slug).Scan(&id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			err = tx.QueryRowContext(ctx, s.q("INSERT INTO "+s.t("roles")+" (name, slug, description, is_system, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id"),
				role.Name, role.Slug, role.Description, true, now, now).Scan(&id)
		case err == nil:
			_, err = tx.ExecContext(ctx, s.q("UPDATE "+s.t("roles")+" SET name = ?, description = ?, is_system = ?, updated_at = ? WHERE id = ?"),
				role.Name, role.Description, true, now, id)
		}
		if err != nil {
			return fmt.Errorf("seeding role %s: %w", role.Slug, err)
		}
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t("role_permission")+" WHERE role_id = ?"), id); err != nil {
			return err
		}
		for _, slug := range expand(role.Permissions) {
			if _, err := tx.ExecContext(ctx, s.q("INSERT INTO "+s.t("role_permission")+" (role_id, permission_id) VALUES (?, ?)"), id, ids[slug]); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// PermissionsOf returns the union of the user's roles' permission slugs,
// sorted. A user with no role has none.
func (s *Store) PermissionsOf(ctx context.Context, userID models.UserID) ([]string, error) {
	out := []string{}
	if userID.Empty() {
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT DISTINCT p.slug FROM "+s.t("role_user")+" ru"+
		" JOIN "+s.t("role_permission")+" rp ON rp.role_id = ru.role_id"+
		" JOIN "+s.t("permissions")+" p ON p.id = rp.permission_id"+
		" WHERE ru.user_id = ? ORDER BY p.slug"), string(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}

// Assign gives the user the role with the given slug (escalated-laravel's
// escalated:roles:assign), keeping the roles they hold. Use it to bootstrap
// the first admin before switching roles & permissions on.
func (s *Store) Assign(ctx context.Context, userID models.UserID, roleSlug string) error {
	if userID.Empty() {
		return errors.New("escalated: a user id is required")
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, s.q("SELECT id FROM "+s.t("roles")+" WHERE slug = ?"), roleSlug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrUnknownRole, roleSlug)
	}
	if err != nil {
		return err
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM "+s.t("role_user")+" WHERE user_id = ? AND role_id = ?"), string(userID), id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.DB.ExecContext(ctx, s.q("INSERT INTO "+s.t("role_user")+" (user_id, role_id) VALUES (?, ?)"), string(userID), id)
	return err
}

// Roles lists every role with its users_count, by name.
func (s *Store) Roles(ctx context.Context) ([]Role, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT r.id, r.name, r.slug, r.description, r.is_system, r.created_at, r.updated_at,"+
		" (SELECT COUNT(*) FROM "+s.t("role_user")+" ru WHERE ru.role_id = r.id)"+
		" FROM "+s.t("roles")+" r ORDER BY r.name, r.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &r.Description, &r.IsSystem, &r.CreatedAt, &r.UpdatedAt, &r.UsersCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Role returns one role with its permissions, or nil when there is none.
func (s *Store) Role(ctx context.Context, id int64) (*Role, error) {
	var r Role
	err := s.DB.QueryRowContext(ctx, s.q("SELECT id, name, slug, description, is_system, created_at, updated_at FROM "+s.t("roles")+" WHERE id = ?"), id).
		Scan(&r.ID, &r.Name, &r.Slug, &r.Description, &r.IsSystem, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT p.id, p.name, p.slug, p."+`"group"`+", p.description FROM "+s.t("permissions")+" p"+
		" JOIN "+s.t("role_permission")+" rp ON rp.permission_id = p.id WHERE rp.role_id = ? ORDER BY p.id"), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	r.Permissions = []Permission{}
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Group, &p.Description); err != nil {
			return nil, err
		}
		r.Permissions = append(r.Permissions, p)
	}
	return &r, rows.Err()
}

// AllPermissions lists every permission, by id.
func (s *Store) AllPermissions(ctx context.Context) ([]Permission, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, name, slug, "+`"group"`+", description FROM "+s.t("permissions")+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Permission{}
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Group, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// KnownPermissionIDs reports which of ids are permissions.
func (s *Store) KnownPermissionIDs(ctx context.Context, ids []int64) (map[int64]bool, error) {
	all, err := s.AllPermissions(ctx)
	if err != nil {
		return nil, err
	}
	known := map[int64]bool{}
	for _, p := range all {
		known[p.ID] = true
	}
	out := map[int64]bool{}
	for _, id := range ids {
		if known[id] {
			out[id] = true
		}
	}
	return out, nil
}

// SaveRole creates (id 0) or updates a role's name and description and
// replaces its permissions. It returns the role id. The slug of a new role is
// made from its name.
func (s *Store) SaveRole(ctx context.Context, id int64, name string, description *string, permissionIDs []int64) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	if id == 0 {
		slug, err := s.uniqueSlug(ctx, tx, name)
		if err != nil {
			return 0, err
		}
		if err := tx.QueryRowContext(ctx, s.q("INSERT INTO "+s.t("roles")+" (name, slug, description, is_system, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id"),
			name, slug, description, false, now, now).Scan(&id); err != nil {
			return 0, err
		}
	} else if _, err := tx.ExecContext(ctx, s.q("UPDATE "+s.t("roles")+" SET name = ?, description = ?, updated_at = ? WHERE id = ?"), name, description, now, id); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t("role_permission")+" WHERE role_id = ?"), id); err != nil {
		return 0, err
	}
	seen := map[int64]bool{}
	for _, pid := range permissionIDs {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if _, err := tx.ExecContext(ctx, s.q("INSERT INTO "+s.t("role_permission")+" (role_id, permission_id) VALUES (?, ?)"), id, pid); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func (s *Store) uniqueSlug(ctx context.Context, tx *sql.Tx, name string) (string, error) {
	base := slugify(name)
	if base == "" {
		base = "role"
	}
	slug := base
	for i := 2; ; i++ {
		var n int
		if err := tx.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM "+s.t("roles")+" WHERE slug = ?"), slug).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// DeleteRole deletes a role and its assignments.
func (s *Store) DeleteRole(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"role_permission", "role_user"} {
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t(table)+" WHERE role_id = ?"), id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t("roles")+" WHERE id = ?"), id); err != nil {
		return err
	}
	return tx.Commit()
}

// UserRoleSlugs returns each user's role slugs, sorted.
func (s *Store) UserRoleSlugs(ctx context.Context, userIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(userIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(userIDs))
	for i, id := range userIDs {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(userIDs)), ",")
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT ru.user_id, r.slug FROM "+s.t("role_user")+" ru JOIN "+s.t("roles")+" r ON r.id = ru.role_id WHERE ru.user_id IN ("+placeholders+")"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid models.UserID
		var slug string
		if err := rows.Scan(&uid, &slug); err != nil {
			return nil, err
		}
		out[string(uid)] = append(out[string(uid)], slug)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out, rows.Err()
}

// RolesGrant reports whether any of the roles holds the permission.
func (s *Store) RolesGrant(ctx context.Context, roleIDs []int64, slug string) (bool, error) {
	for _, id := range roleIDs {
		var n int
		if err := s.DB.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM "+s.t("role_permission")+" rp JOIN "+s.t("permissions")+" p ON p.id = rp.permission_id WHERE rp.role_id = ? AND p.slug = ?"), id, slug).Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// ReplaceUserRoles sets the user's roles to exactly roleIDs.
func (s *Store) ReplaceUserRoles(ctx context.Context, userID models.UserID, roleIDs []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t("role_user")+" WHERE user_id = ?"), string(userID)); err != nil {
		return err
	}
	for _, id := range roleIDs {
		if _, err := tx.ExecContext(ctx, s.q("INSERT INTO "+s.t("role_user")+" (user_id, role_id) VALUES (?, ?)"), string(userID), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
