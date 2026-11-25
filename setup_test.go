package escalated

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
)

func TestSetupCheckNamesEveryUnsetCallback(t *testing.T) {
	db := testdb.Open(t)
	want := []string{SetupProblemAgentCheck, SetupProblemAdminCheck, SetupProblemUserIDFunc}

	// DefaultConfig's placeholders left in place, and a bare Config.
	for name, cfg := range map[string]Config{"DefaultConfig": DefaultConfig(), "Config{}": {}} {
		t.Run(name, func(t *testing.T) {
			cfg.DB = db
			esc, err := New(cfg)
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			defer esc.Close()
			got := esc.SetupCheck()
			if !slices.Equal(got, want) {
				t.Fatalf("SetupCheck() = %q, want %q", got, want)
			}
		})
	}

	for _, problem := range want {
		field := strings.Fields(problem)[0]
		if !strings.HasPrefix(field, "Config.") || !strings.Contains(problem, "Set it to") {
			t.Errorf("problem %q does not name the field and how to set it", problem)
		}
	}
}

func TestSetupCheckIsEmptyOnceTheHostWiresItsUsers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.AgentCheck = func(r *http.Request) bool { return r.Header.Get("X-Role") == "agent" }
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get("X-Role") == "admin" }
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get("X-User")) }
	esc, err := New(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer esc.Close()
	if got := esc.SetupCheck(); len(got) != 0 {
		t.Fatalf("SetupCheck() = %q, want nothing", got)
	}

	// Only the admin check missing: only it is reported.
	cfg.AdminCheck = nil
	esc2, err := New(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer esc2.Close()
	if got := esc2.SetupCheck(); !slices.Equal(got, []string{SetupProblemAdminCheck}) {
		t.Fatalf("SetupCheck() = %q", got)
	}
}
