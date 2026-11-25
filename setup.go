package escalated

import (
	"net/http"
	"reflect"

	"github.com/escalated-dev/escalated-go/models"
)

// noStaffCheck is the AgentCheck and AdminCheck a host has not set: nobody is
// staff, so every agent and admin route answers 403.
func noStaffCheck(*http.Request) bool { return false }

// noUserID is the UserIDFunc a host has not set: nobody is signed in.
func noUserID(*http.Request) models.UserID { return "" }

// Setup problems SetupCheck reports. Escalated owns no users: it reads who is
// signed in and who is staff only through these three callbacks, so an
// unset one does not fail loudly, it refuses everyone.
const (
	SetupProblemAgentCheck = "Config.AgentCheck is not set, so nobody is an agent: every agent page and the agent JSON API answer 403. " +
		"Set it to your own check that the signed-in user is a support agent, e.g. cfg.AgentCheck = func(r *http.Request) bool { return currentUser(r).IsAgent } (docs/getting-started.md)."
	SetupProblemAdminCheck = "Config.AdminCheck is not set, so nobody is an admin: every admin page answers 403. " +
		"Set it to your own check that the signed-in user is a support admin, e.g. cfg.AdminCheck = func(r *http.Request) bool { return currentUser(r).IsAdmin } (docs/getting-started.md)."
	SetupProblemUserIDFunc = "Config.UserIDFunc is not set, so Escalated cannot tell who is signed in: customer pages answer 401 and staff replies, assignments and activity are recorded with no user. " +
		"Set it to return the signed-in user's id from your session, e.g. cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(currentUser(r).ID) } (docs/getting-started.md)."
)

// SetupCheck reports what the host has not set up that Escalated needs: the
// staff checks (Config.AgentCheck, Config.AdminCheck) and the signed-in user
// (Config.UserIDFunc). New logs each problem once at startup; an install or
// health script can call it too. Empty when nothing is missing.
//
// Unlike escalated-laravel, there is no user model to implement: Go's types
// already make the host return a bool and a models.UserID, so a host user that
// lacks a contract cannot fail at run time. What can go wrong is a callback
// left unset, which refuses everyone without saying why.
func (e *Escalated) SetupCheck() []string {
	return append([]string(nil), e.setupProblems...)
}

// setupProblems inspects the Config a host passed to New, before defaults
// fill the unset callbacks in.
func setupProblems(cfg Config) []string {
	var problems []string
	if cfg.AgentCheck == nil || sameFunc(cfg.AgentCheck, noStaffCheck) {
		problems = append(problems, SetupProblemAgentCheck)
	}
	if cfg.AdminCheck == nil || sameFunc(cfg.AdminCheck, noStaffCheck) {
		problems = append(problems, SetupProblemAdminCheck)
	}
	if cfg.UserIDFunc == nil || sameFunc(cfg.UserIDFunc, noUserID) {
		problems = append(problems, SetupProblemUserIDFunc)
	}
	return problems
}

// sameFunc reports whether f is the package function g (DefaultConfig's
// placeholder left in place).
func sameFunc(f, g any) bool {
	return reflect.ValueOf(f).Pointer() == reflect.ValueOf(g).Pointer()
}
