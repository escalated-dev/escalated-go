package services

import (
	"encoding/json"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

type auditLogStoreStub struct {
	log *models.AuditLog
}

func (s *auditLogStoreStub) CreateAuditLog(log *models.AuditLog) error {
	cp := *log
	s.log = &cp
	return nil
}

func (s *auditLogStoreStub) ListAuditLogsByEntity(_ string, _ int64, _ int) ([]*models.AuditLog, error) {
	return nil, nil
}

func (s *auditLogStoreStub) ListAuditLogsByPerformer(_ string, _ models.UserID, _ int) ([]*models.AuditLog, error) {
	return nil, nil
}

func TestAuditLogServiceRedactsNestedSensitiveValues(t *testing.T) {
	store := &auditLogStoreStub{}
	service := NewAuditLogService(store)
	original := map[string]interface{}{
		"name":     "Jane",
		"password": "plain-password",
		"profile": map[string]interface{}{
			"apiKey": "api-key-value",
			"nested": []interface{}{
				map[string]interface{}{"token": "token-value"},
				map[string]interface{}{"safe": "visible"},
			},
		},
		"credentials": map[string]interface{}{
			"username": "jane",
			"secret":   "secret-value",
		},
	}

	if err := service.Log("updated", "user", nil, nil, nil, nil, original); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(store.log.NewValues, &got); err != nil {
		t.Fatalf("NewValues JSON invalid: %v", err)
	}

	if got["password"] != auditRedactedValue {
		t.Fatalf("password = %v, want redacted", got["password"])
	}

	profile := got["profile"].(map[string]interface{})
	if profile["apiKey"] != auditRedactedValue {
		t.Fatalf("apiKey = %v, want redacted", profile["apiKey"])
	}

	nested := profile["nested"].([]interface{})
	nestedToken := nested[0].(map[string]interface{})["token"]
	if nestedToken != auditRedactedValue {
		t.Fatalf("nested token = %v, want redacted", nestedToken)
	}

	credentials := got["credentials"]
	if credentials != auditRedactedValue {
		t.Fatalf("credentials = %v, want redacted", credentials)
	}
}

func TestAuditLogServiceDoesNotMutateOriginalValues(t *testing.T) {
	store := &auditLogStoreStub{}
	service := NewAuditLogService(store)
	values := map[string]interface{}{
		"token": "original-token",
		"nested": map[string]interface{}{
			"secret": "original-secret",
		},
	}

	if err := service.Log("updated", "user", nil, nil, nil, values, nil); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	if values["token"] != "original-token" {
		t.Fatalf("original token mutated: %v", values["token"])
	}

	nested := values["nested"].(map[string]interface{})
	if nested["secret"] != "original-secret" {
		t.Fatalf("original nested secret mutated: %v", nested["secret"])
	}
}
