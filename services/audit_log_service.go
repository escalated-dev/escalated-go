package services

import (
	"encoding/json"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

const auditRedactedValue = "[REDACTED]"

var auditSensitiveKeyFragments = []string{
	"api_key",
	"apikey",
	"authorization",
	"credential",
	"password",
	"recovery_code",
	"secret",
	"token",
}

// AuditLogStore defines the persistence interface for audit logs.
type AuditLogStore interface {
	CreateAuditLog(log *models.AuditLog) error
	ListAuditLogsByEntity(entityType string, entityID int64, limit int) ([]*models.AuditLog, error)
	ListAuditLogsByPerformer(performerType string, performerID models.UserID, limit int) ([]*models.AuditLog, error)
}

// AuditLogService records and queries audit trail entries.
type AuditLogService struct {
	store AuditLogStore
}

// NewAuditLogService creates a new AuditLogService.
func NewAuditLogService(store AuditLogStore) *AuditLogService {
	return &AuditLogService{store: store}
}

// Log records an audit entry.
func (s *AuditLogService) Log(action, entityType string, entityID *int64, performerType *string, performerID *models.UserID, oldValues, newValues interface{}) error {
	entry := &models.AuditLog{
		Action:        action,
		EntityType:    entityType,
		EntityID:      entityID,
		PerformerType: performerType,
		PerformerID:   performerID,
	}

	if oldValues != nil {
		data, _ := marshalAuditValues(oldValues)
		entry.OldValues = data
	}
	if newValues != nil {
		data, _ := marshalAuditValues(newValues)
		entry.NewValues = data
	}

	return s.store.CreateAuditLog(entry)
}

// Record logs an admin change made through a request (Laravel's Auditable
// trait): who made it, from which address and client, and the old and new
// values (sensitive keys redacted). performerID may be empty for system
// changes.
func (s *AuditLogService) Record(action, entityType string, entityID int64, performerID models.UserID, ip, userAgent string, oldValues, newValues interface{}) error {
	if s == nil || s.store == nil {
		return nil
	}
	entry := &models.AuditLog{Action: action, EntityType: entityType, EntityID: &entityID}
	if !performerID.Empty() {
		pt := "user"
		entry.PerformerType = &pt
		entry.PerformerID = &performerID
	}
	if ip != "" {
		entry.IPAddress = &ip
	}
	if userAgent != "" {
		if len(userAgent) > 255 {
			userAgent = userAgent[:255]
		}
		entry.UserAgent = &userAgent
	}
	if oldValues != nil {
		entry.OldValues, _ = marshalAuditValues(oldValues)
	}
	if newValues != nil {
		entry.NewValues, _ = marshalAuditValues(newValues)
	}
	return s.store.CreateAuditLog(entry)
}

// LogsForEntity returns audit entries for a specific entity.
func (s *AuditLogService) LogsForEntity(entityType string, entityID int64, limit int) ([]*models.AuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListAuditLogsByEntity(entityType, entityID, limit)
}

// LogsByPerformer returns audit entries by a specific performer.
func (s *AuditLogService) LogsByPerformer(performerType string, performerID models.UserID, limit int) ([]*models.AuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListAuditLogsByPerformer(performerType, performerID, limit)
}

func marshalAuditValues(values interface{}) ([]byte, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return data, err
	}

	var decoded interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return data, nil
	}

	redactAuditValue(decoded)

	return json.Marshal(decoded)
}

func redactAuditValue(value interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			if isAuditSensitiveKey(key) {
				typed[key] = auditRedactedValue
				continue
			}

			redactAuditValue(nested)
		}
	case []interface{}:
		for _, nested := range typed {
			redactAuditValue(nested)
		}
	}
}

func isAuditSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))

	for _, fragment := range auditSensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}

	return false
}
