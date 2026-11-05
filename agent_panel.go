package escalated

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
)

// AgentPanelConfig configures the agent panel's data (escalated-developer-
// context ADR 2026-11-05-agent-panel-data; docs/agent-panel.md).
type AgentPanelConfig struct {
	// DisableNotifications switches the agents' in-app notifications off:
	// nothing is stored, the feed routes answer 404 and the shared unread
	// count is left out.
	DisableNotifications bool

	// PresenceOnlineWindow is how long after the customer portal or the
	// widget last saw a customer the agent pages show them as online.
	// Defaults to 120 seconds.
	PresenceOnlineWindow time.Duration

	// Broadcaster, when set, receives each new notification as
	// notification.created on the agent's private channel
	// (services.UserChannel). Nil falls back to Config.Plugins.Broadcaster;
	// with neither, nothing is broadcast.
	Broadcaster *services.Broadcaster
}

// AgentPanel returns the agent panel's data service: agent statuses, unread
// markers, customer presence and the agents' in-app notifications. Nil
// without a database.
func (e *Escalated) AgentPanel() *services.AgentPanel {
	if e.Config.DB == nil {
		return nil
	}
	e.agentPanelOnce.Do(func() {
		cfg := e.Config
		broadcaster := cfg.AgentPanel.Broadcaster
		if broadcaster == nil {
			broadcaster = cfg.Plugins.Broadcaster
		}
		e.agentPanel = &services.AgentPanel{
			DB:                    cfg.DB,
			Prefix:                cfg.TablePrefix,
			Access:                e.Access,
			Broadcaster:           broadcaster,
			NotificationsDisabled: cfg.AgentPanel.DisableNotifications,
			OnlineWindow:          cfg.AgentPanel.PresenceOnlineWindow,
			RoutePrefix:           cfg.RoutePrefix,
		}
		if dir := cfg.SkillAgentDirectory; dir != nil {
			// The staff a mention can name: the host's agent directory.
			e.agentPanel.Agents = func(ctx context.Context) ([]services.MentionAgent, error) {
				rows, err := dir.ListAgentsForSkillForm(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]services.MentionAgent, 0, len(rows))
				for _, row := range rows {
					out = append(out, services.MentionAgent{ID: models.UserID(strconv.FormatInt(row.ID, 10)), Name: row.Name, Email: row.Email})
				}
				return out, nil
			}
		}
	})
	return e.agentPanel
}

// agentPanelShare shares the agent panel's data with every staff page:
// escalated.agent_status, escalated.agent_statuses and, while notifications
// are on, escalated.notifications.unread_count. Customers and guests get none
// of it.
func (e *Escalated) agentPanelShare(next http.Handler) http.Handler {
	cfg := e.Config
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panel := e.AgentPanel()
		if panel == nil || (!cfg.AgentCheck(r) && !cfg.AdminCheck(r)) {
			next.ServeHTTP(w, r)
			return
		}
		uid := cfg.UserIDFunc(r)
		if uid.Empty() {
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		ctx = renderer.WithShared(ctx, "escalated", func() any {
			shared := map[string]any{
				"agent_status":   panel.Status(ctx, uid),
				"agent_statuses": services.AgentStatuses,
			}
			if panel.NotificationsEnabled() {
				shared["notifications"] = map[string]any{"unread_count": panel.UnreadCount(ctx, uid)}
			}
			return shared
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CustomerPresenceMiddleware records that the signed-in customer is on the
// site, for the agent pages' requester_online (at most every 30 seconds).
// Staff browsing the customer portal are not recorded. The routers mount it
// on the customer portal's routes.
func (e *Escalated) CustomerPresenceMiddleware(next http.Handler) http.Handler {
	cfg := e.Config
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if panel := e.AgentPanel(); panel != nil && !cfg.AgentCheck(r) && !cfg.AdminCheck(r) {
			panel.SeeUser(r.Context(), cfg.UserIDFunc(r))
		}
		next.ServeHTTP(w, r)
	})
}

// ChatAgentOnline reports whether an agent may get a new live chat: their own
// status (ADR 2026-11-05-agent-panel-data) is "online", as escalated-laravel's
// chat routing reads agent_profiles.chat_status. Away, offline and agents who
// never set a status get no chats. Config.ChatAgentOnline, when set, must
// also say yes (a host's own presence check). Without a database only the
// host hook decides (nil: every agent).
func (e *Escalated) ChatAgentOnline() func(ctx context.Context, agentID models.UserID) (bool, error) {
	panel := e.AgentPanel()
	hook := e.Config.ChatAgentOnline
	if panel == nil {
		return hook
	}
	return func(ctx context.Context, agentID models.UserID) (bool, error) {
		if panel.Status(ctx, agentID) != "online" {
			return false, nil
		}
		if hook == nil {
			return true, nil
		}
		return hook(ctx, agentID)
	}
}
