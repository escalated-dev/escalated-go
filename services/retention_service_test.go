package services

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/store"
)

func TestRetentionDays(t *testing.T) {
	cases := map[string]struct {
		days int
		ok   bool
	}{
		"never":    {0, false},
		"":         {0, false},
		"bogus":    {0, false},
		"90_days":  {90, true},
		"180_days": {180, true},
		"1_year":   {365, true},
		"2_years":  {730, true},
		"5_years":  {1825, true},
	}
	for setting, want := range cases {
		days, ok := RetentionDays(setting)
		if days != want.days || ok != want.ok {
			t.Errorf("RetentionDays(%q) = (%d, %v), want (%d, %v)", setting, days, ok, want.days, want.ok)
		}
	}
}

func TestRetentionCutoff(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cutoff, ok := RetentionCutoff("90_days", now)
	if !ok {
		t.Fatal("expected retention enabled for 90_days")
	}
	if want := now.AddDate(0, 0, -90); !cutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v", cutoff, want)
	}

	if _, ok := RetentionCutoff("never", now); ok {
		t.Error("never should be disabled")
	}
}

func TestPurgeExpired(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatal(err)
	}
	s := store.NewSQLiteStore(db, "escalated_")
	ctx := context.Background()

	if err := s.SetSetting(ctx, "retention_attachments", "90_days"); err != nil {
		t.Fatal(err)
	}

	for _, ts := range []time.Time{time.Now().AddDate(0, 0, -200), time.Now().AddDate(0, 0, -10)} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO escalated_attachments (ticket_id, original_filename, mime_type, storage_path, created_at)
			 VALUES (1, 'f', 'text/plain', '/p', ?)`, ts); err != nil {
			t.Fatal(err)
		}
	}

	rs := NewRetentionService(db, s)
	count := func() int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(1) FROM escalated_attachments`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Dry run reports the one expired attachment but deletes nothing.
	rep, err := rs.PurgeExpired(ctx, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.AttachmentsDeleted != 1 {
		t.Fatalf("dry-run: want 1 candidate, got %d", rep.AttachmentsDeleted)
	}
	if count() != 2 {
		t.Fatalf("dry-run must not delete; have %d rows", count())
	}

	// Real run deletes the expired attachment, keeps the recent one.
	rep, err = rs.PurgeExpired(ctx, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.AttachmentsDeleted != 1 {
		t.Fatalf("want 1 deleted, got %d", rep.AttachmentsDeleted)
	}
	if count() != 1 {
		t.Fatalf("want 1 remaining, got %d", count())
	}
}
