package router

import (
	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/services"
)

// uiPageDeps are the services the shared pages read from besides the store.
type uiPageDeps struct {
	subjects *services.TicketSubjectService
	macros   *services.MacroService
	canned   *services.CannedResponseService
}

// wireUIPages gives the page handlers what they need to build the props the
// shared Vue pages read: the database for counts, the host's user lookups,
// the route prefix for links, and the ticket screen's panels.
func wireUIPages(esc *escalated.Escalated, agentH *handlers.AgentHandler, customerH *handlers.CustomerHandler, adminH *handlers.AdminHandler, deps uiPageDeps) {
	cfg := esc.Config
	users := handlers.UserLookup{Name: cfg.UserName, Email: cfg.UserEmail}

	agentH.DB = cfg.DB
	agentH.TablePrefix = cfg.TablePrefix
	agentH.Users = users
	agentH.RoutePrefix = cfg.RoutePrefix
	agentH.Subjects = deps.subjects
	agentH.Macros = deps.macros
	agentH.CannedResponses = deps.canned

	customerH.DB = cfg.DB
	customerH.TablePrefix = cfg.TablePrefix
	customerH.Users = users
	customerH.RoutePrefix = cfg.RoutePrefix

	adminH.DB = cfg.DB
	adminH.TablePrefix = cfg.TablePrefix
}
