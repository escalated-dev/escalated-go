package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
)

// The plugin contract's two tables (escalated-developer-context
// domain-model/plugin-contract.md): escalated_plugins holds activation, one
// row per slug, and escalated_plugin_store holds what ctx.store and
// ctx.config write. Runs on whichever database the suite was asked for, so
// both migration sets are exercised.
func TestMigrateCreatesThePluginTables(t *testing.T) {
	db := testdb.Open(t)
	exec := func(q string, args ...any) error {
		_, err := db.Exec(sqldialect.Rebind(db, q), args...)
		return err
	}

	now := time.Now().UTC()
	if err := exec(`INSERT INTO escalated_plugins (slug, is_active, activated_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"escalated-dev--plugin-slack", true, now, now, now); err != nil {
		t.Fatalf("insert plugin row: %v", err)
	}
	if err := exec(`INSERT INTO escalated_plugins (slug) VALUES (?)`, "escalated-dev--plugin-slack"); err == nil {
		t.Error("a second row for one slug was accepted; slug must be unique")
	}

	var active bool
	var deactivated any
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT is_active, deactivated_at FROM escalated_plugins WHERE slug = ?`),
		"escalated-dev--plugin-slack").Scan(&active, &deactivated); err != nil {
		t.Fatalf("read plugin row: %v", err)
	}
	if !active || deactivated != nil {
		t.Errorf("row is_active=%v deactivated_at=%v", active, deactivated)
	}

	// A new row is inactive by default.
	if err := exec(`INSERT INTO escalated_plugins (slug) VALUES (?)`, "acme--crm"); err != nil {
		t.Fatalf("insert default row: %v", err)
	}
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT is_active FROM escalated_plugins WHERE slug = ?`), "acme--crm").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active {
		t.Error("is_active defaults to true; the contract says false")
	}

	// The store keys by plugin name, collection and a nullable key, with the
	// lengths the contract asks for.
	long := strings.Repeat("k", 191)
	if err := exec(`INSERT INTO escalated_plugin_store (plugin, collection, key, data) VALUES (?, ?, ?, ?)`,
		"@escalated-dev/plugin-slack", strings.Repeat("c", 100), long, `{"a":1}`); err != nil {
		t.Fatalf("insert store row: %v", err)
	}
	if err := exec(`INSERT INTO escalated_plugin_store (plugin, collection, key, data) VALUES (?, ?, NULL, ?)`,
		"@escalated-dev/plugin-slack", "events", `{"b":2}`); err != nil {
		t.Fatalf("insert store row without a key: %v", err)
	}
	var data string
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT data FROM escalated_plugin_store WHERE plugin = ? AND collection = ? AND key = ?`),
		"@escalated-dev/plugin-slack", strings.Repeat("c", 100), long).Scan(&data); err != nil {
		t.Fatalf("read store row: %v", err)
	}
	if !strings.Contains(data, `"a"`) {
		t.Errorf("data %q", data)
	}
}
