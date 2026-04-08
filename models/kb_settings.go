package models

// KBSettings holds configuration for the knowledge base feature.
type KBSettings struct {
	KnowledgeBaseEnabled bool `json:"knowledge_base_enabled"`
	Public               bool `json:"public"`
	FeedbackEnabled      bool `json:"feedback_enabled"`
}

// DefaultKBSettings returns KB settings with sensible defaults.
func DefaultKBSettings() KBSettings {
	return KBSettings{
		KnowledgeBaseEnabled: false,
		Public:               true,
		FeedbackEnabled:      true,
	}
}
