package escalated

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Escalated's tables live on whatever *sql.DB the host hands to Config.DB. That
// may be a database the host application otherwise never touches -- a schema
// shared with a legacy system, a separate reporting store, or simply a database
// it would rather not mix support data into.
//
// The separation only holds while no query in this package reaches for a table
// the package does not own. A host table queried through Config.DB does not
// fail loudly on a separate database; it returns no rows, which reads as users
// who do not exist.
//
// These tests read the package source. That is the only place the guarantee can
// be checked: a runtime test would need a host schema to be wrong about.

var (
	// Table names are always built through a prefix helper -- t("tickets"),
	// TableName("replies"), cfg.TablePrefix+"tags". A bare quoted table name in
	// a statement is a table this package does not own.
	fromOrJoin = regexp.MustCompile(`(?i)\b(?:from|join|into|update)\s+([a-z_][a-z0-9_]*)`)

	// Tables the host owns. Escalated stores user ids as plain unconstrained
	// columns precisely so these can live on a different database.
	hostTables = []string{"users", "accounts", "user", "members", "people"}
)

func packageSources(t *testing.T) map[string]string {
	t.Helper()

	sources := map[string]string{}

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "docker" || info.Name() == "templates" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sources[filepath.ToSlash(path)] = string(body)

		return nil
	})
	if err != nil {
		t.Fatalf("walking package sources: %v", err)
	}

	if len(sources) == 0 {
		t.Fatal("found no package sources to check")
	}

	return sources
}

func TestNoSQLReferencesAHostOwnedTable(t *testing.T) {
	sources := packageSources(t)

	for path, body := range sources {
		for _, match := range fromOrJoin.FindAllStringSubmatch(body, -1) {
			table := strings.ToLower(match[1])

			for _, host := range hostTables {
				if table != host {
					continue
				}

				t.Errorf(
					"%s issues SQL against %q, a table the host owns.\n"+
						"Config.DB may be a different database entirely; reach host data "+
						"through the UserDirectory / SkillAgentDirectory interfaces instead.",
					path, table,
				)
			}
		}
	}
}

func TestHostUserDataIsReachedThroughInterfacesOnly(t *testing.T) {
	// The two seams through which the host exposes its users. Both are plain
	// interfaces the host implements against its own connection, which is why a
	// user lookup never travels over Config.DB.
	cfg := DefaultConfig()

	if cfg.UserDirectory != nil {
		t.Error("DefaultConfig must not assume a UserDirectory; a host without one gets an empty admin users page, not a query against its database")
	}

	if cfg.SkillAgentDirectory != nil {
		t.Error("DefaultConfig must not assume a SkillAgentDirectory")
	}

	if cfg.DB != nil {
		t.Error("DefaultConfig must not assume a database; the host supplies the connection")
	}
}
