package services

import (
	"encoding/json"

	"github.com/escalated-dev/escalated-go/models"
)

// AuditLogStore defines the persistence interface for audit logs.
type AuditLogStore interface {
	CreateAuditLog(log *models.AuditLog) error
	ListAuditLogsByEntity(entityType string, entityID int64, limit int) ([]*models.AuditLog, error)
	ListAuditLogsByPerformer(performerType string, performerID int64, limit int) ([]*models.AuditLog, error)
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
func (s *AuditLogService) Log(action, entityType string, entityID *int64, performerType *string, performerID *int64, oldValues, newValues interface{}) error {
	entry := &models.AuditLog{
		Action:        action,
		EntityType:    entityType,
		EntityID:      entityID,
		PerformerType: performerType,
		PerformerID:   performerID,
	}

	if oldValues != nil {
		data, _ := json.Marshal(oldValues)
		entry.OldValues = data
	}
	if newValues != nil {
		data, _ := json.Marshal(newValues)
		entry.NewValues = data
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
func (s *AuditLogService) LogsByPerformer(performerType string, performerID int64, limit int) ([]*models.AuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListAuditLogsByPerformer(performerType, performerID, limit)
}
