// Package ticketaccess provides opt-in request scopes and host account hooks.
package ticketaccess

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"regexp"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"

	"github.com/escalated-dev/escalated-go/models"
)

type Account struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type Reference struct {
	Kind, Type, ID string
	Model          any
}
type User struct {
	ID           models.UserID
	Agent, Admin bool
	Model        any
}
type AccountConfig struct {
	Enabled     bool
	ScopeAgents bool
	Resolver    func(context.Context, Reference) (*Account, error)
}
type Options struct {
	Visibility   string
	Accounts     AccountConfig
	TargetedSLAs bool
	ResolveUser  func(context.Context, models.UserID) (*User, error)
}
type Access struct {
	DB      *sql.DB
	Prefix  string
	Options Options
}
type Actor struct {
	UserID                models.UserID
	Departments, Accounts bool
	Account               *Account
	Prefix                string
}
type actorKey struct{}
type accessKey struct{}

func Current(ctx context.Context) *Actor { actor, _ := ctx.Value(actorKey{}).(*Actor); return actor }
func WithoutVisibility(ctx context.Context) context.Context {
	return context.WithValue(ctx, actorKey{}, (*Actor)(nil))
}
func WithActor(ctx context.Context, actor *Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}
func New(db *sql.DB, prefix string, options Options) (*Access, error) {
	if options.Visibility == "" {
		options.Visibility = "all"
	}
	if options.Visibility != "all" && options.Visibility != "department" {
		return nil, fmt.Errorf("visibility must be all or department")
	}
	if options.Accounts.ScopeAgents && (!options.Accounts.Enabled || options.Accounts.Resolver == nil) {
		return nil, fmt.Errorf("account visibility requires enabled accounts and a resolver")
	}
	if !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(prefix) {
		return nil, fmt.Errorf("invalid table prefix")
	}
	return &Access{DB: db, Prefix: prefix, Options: options}, nil
}
func (a *Access) Restricted() bool {
	return a != nil && (a.Options.Visibility == "department" || a.Options.Accounts.ScopeAgents)
}
func (a *Access) ResolveAccount(ctx context.Context, ref Reference) (*Account, error) {
	if a == nil || !a.Options.Accounts.Enabled || a.Options.Accounts.Resolver == nil {
		return nil, nil
	}
	account, err := a.Options.Accounts.Resolver(ctx, ref)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Type == "" || account.ID == "" || len(account.Type) > 191 || len(account.ID) > 191 {
		return nil, nil
	}
	return account, nil
}
func (a *Access) ForUser(ctx context.Context, user User) (context.Context, error) {
	if a != nil {
		ctx = context.WithValue(ctx, accessKey{}, a)
	}
	if !a.Restricted() || user.Admin || !user.Agent {
		return WithoutVisibility(ctx), nil
	}
	var account *Account
	var err error
	if a.Options.Accounts.ScopeAgents {
		account, err = a.ResolveAccount(ctx, Reference{Kind: "agent", Type: "User", ID: string(user.ID), Model: user.Model})
	}
	if err != nil {
		return nil, err
	}
	return WithActor(ctx, &Actor{UserID: user.ID, Departments: a.Options.Visibility == "department", Accounts: a.Options.Accounts.ScopeAgents, Account: account, Prefix: a.Prefix}), nil
}
func (a *Access) Middleware(agentCheck, adminCheck func(*http.Request) bool, userID func(*http.Request) models.UserID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !a.Restricted() {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, err := a.ForUser(r.Context(), User{ID: userID(r), Agent: agentCheck(r), Admin: adminCheck(r), Model: r})
			if err != nil {
				http.Error(w, "Unable to resolve ticket access", http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CanViewUser reloads the host identity and current membership at delivery time.
func (a *Access) CanViewUser(ctx context.Context, id models.UserID, ticketID int64, staffOnly bool) bool {
	if !a.Restricted() {
		return true
	}
	if a.Options.ResolveUser == nil {
		return false
	}
	user, err := a.Options.ResolveUser(WithoutVisibility(ctx), id)
	if err != nil || user == nil || user.ID != id || user.ID.Empty() {
		return false
	}
	if staffOnly && !user.Agent && !user.Admin {
		return false
	}
	scoped, err := a.ForUser(WithoutVisibility(ctx), *user)
	if err != nil {
		return false
	}
	q := "SELECT 1 FROM " + a.Prefix + "tickets WHERE id = ?"
	args := []any{ticketID}
	if !user.Agent && !user.Admin {
		q += " AND CAST(requester_id AS TEXT) = ?"
		args = append(args, string(id))
	}
	var found int
	return QueryRowContext(scoped, a.DB, sqldialect.Rebind(a.DB, q), args...).Scan(&found) == nil
}
func (a *Access) Recipients(ctx context.Context, ticketID int64, ids []models.UserID) []models.UserID {
	result := []models.UserID{}
	seen := map[models.UserID]bool{}
	for _, id := range ids {
		if !seen[id] && a.CanViewUser(ctx, id, ticketID, false) {
			result = append(result, id)
			seen[id] = true
		}
	}
	return result
}
func (a *Access) FillSubjectAccount(ctx context.Context, ticketID int64, subjectType, subjectID string) error {
	account, err := a.ResolveAccount(ctx, Reference{Kind: "subject", Type: subjectType, ID: subjectID})
	if err != nil || account == nil {
		return err
	}
	_, err = ExecContext(ctx, a.DB, sqldialect.Rebind(a.DB, "UPDATE "+a.Prefix+"tickets SET account_type=?, account_id=? WHERE id=? AND account_type IS NULL AND account_id IS NULL"), account.Type, account.ID, ticketID)
	return err
}
