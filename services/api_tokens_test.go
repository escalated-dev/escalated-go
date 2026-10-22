package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
)

// API tokens (escalated-laravel ApiToken): stored as a SHA-256 digest, found
// by the plain text, shown once.
func TestAPITokenServiceStoresOnlyTheDigest(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := &APITokenService{DB: db, Prefix: testdb.Prefix}

	expires := time.Now().Add(48 * time.Hour)
	token, plain, err := svc.Create(ctx, "900", "CI", []string{models.APITokenAbilityAgent}, &expires)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(plain) != 64 {
		t.Fatalf("plain text %q is not 64 hex characters", plain)
	}
	if token.UserID != "900" || token.Name != "CI" || len(token.Abilities) != 1 || token.Abilities[0] != "agent" {
		t.Fatalf("token %+v", token)
	}
	if token.ExpiresAt == nil || token.ExpiresAt.Sub(expires).Abs() > time.Second {
		t.Fatalf("expires_at %v, want %v", token.ExpiresAt, expires)
	}

	var stored string
	if err := db.QueryRow(`SELECT token FROM escalated_api_tokens`).Scan(&stored); err != nil {
		t.Fatalf("stored: %v", err)
	}
	if stored == plain || stored != HashAPIToken(plain) {
		t.Fatalf("stored %q; want the SHA-256 digest of the token, not the token", stored)
	}

	found, err := svc.FindByPlainText(ctx, plain)
	if err != nil || found == nil || found.ID != token.ID {
		t.Fatalf("find by plain text: %+v %v", found, err)
	}
	if miss, _ := svc.FindByPlainText(ctx, stored); miss != nil {
		t.Fatal("the digest itself must not authenticate")
	}
	if !found.HasAbility("agent") || found.HasAbility("admin") {
		t.Errorf("abilities %v", found.Abilities)
	}

	// Use is recorded, then not rewritten inside the interval.
	if err := svc.Touch(ctx, found, "203.0.113.9"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	used, _ := svc.Get(ctx, token.ID)
	if used.LastUsedAt == nil || used.LastUsedIP == nil || *used.LastUsedIP != "203.0.113.9" {
		t.Fatalf("last used %v %v", used.LastUsedAt, used.LastUsedIP)
	}
	if err := svc.Touch(ctx, used, "198.51.100.1"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	again, _ := svc.Get(ctx, token.ID)
	if *again.LastUsedIP != "203.0.113.9" {
		t.Errorf("last_used_ip rewritten inside the interval: %s", *again.LastUsedIP)
	}

	name := "Deploys"
	if ok, err := svc.Update(ctx, token.ID, &name, []string{"*"}); !ok || err != nil {
		t.Fatalf("update: %v %v", ok, err)
	}
	updated, _ := svc.Get(ctx, token.ID)
	if updated.Name != "Deploys" || !updated.HasAbility("admin") {
		t.Errorf("after update %+v", updated)
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %v", list, err)
	}
	if ok, err := svc.Delete(ctx, token.ID); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if gone, _ := svc.FindByPlainText(ctx, plain); gone != nil {
		t.Error("a revoked token still authenticates")
	}
	if ok, _ := svc.Delete(ctx, token.ID); ok {
		t.Error("deleting twice reported a token")
	}
}

func TestAPITokenServiceDefaultsToEveryAbilityAndNoExpiry(t *testing.T) {
	db := testdb.Open(t)
	svc := &APITokenService{DB: db, Prefix: testdb.Prefix}
	token, plain, err := svc.Create(context.Background(), "901", "Desktop", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if strings.Join(token.Abilities, ",") != "*" || token.ExpiresAt != nil || token.IsExpired(time.Now()) {
		t.Errorf("token %+v", token)
	}
	_, other, _ := svc.Create(context.Background(), "901", "Second", nil, nil)
	if other == plain {
		t.Error("two tokens share a plain text")
	}

	past := time.Now().Add(-time.Minute)
	token.ExpiresAt = &past
	if !token.IsExpired(time.Now()) {
		t.Error("a token past its expiry is not expired")
	}
}
