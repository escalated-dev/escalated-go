package models

import "time"

// Skill is a persisted row in escalated_skills.
type Skill struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SkillRoutingTag is a row in escalated_skill_routing_tags.
type SkillRoutingTag struct {
	ID      int64 `json:"id"`
	SkillID int64 `json:"skill_id"`
	TagID   int64 `json:"tag_id"`
}

// SkillRoutingDepartment is a row in escalated_skill_routing_departments.
type SkillRoutingDepartment struct {
	ID           int64 `json:"id"`
	SkillID      int64 `json:"skill_id"`
	DepartmentID int64 `json:"department_id"`
}

// AgentSkill is a row in escalated_agent_skills (user + proficiency).
type AgentSkill struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	SkillID     int64     `json:"skill_id"`
	Proficiency int       `json:"proficiency"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SkillListItem is the index table row shape for Admin/Skills/Index.
type SkillListItem struct {
	ID                      int64     `json:"id"`
	Name                    string    `json:"name"`
	AgentsCount             int       `json:"agents_count"`
	RoutingTagsCount        int       `json:"routing_tags_count"`
	RoutingDepartmentsCount int       `json:"routing_departments_count"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// SkillFormPayload is the nested "skill" object on the create/edit form.
type SkillFormPayload struct {
	ID                   int64               `json:"id"`
	Name                 string              `json:"name"`
	RoutingTagIDs        []int64             `json:"routing_tag_ids"`
	RoutingDepartmentIDs []int64             `json:"routing_department_ids"`
	Agents               []SkillFormAgentRow `json:"agents"`
}

// SkillFormAgentRow is one agent line on the form (user_id + proficiency).
type SkillFormAgentRow struct {
	UserID      int64 `json:"user_id"`
	Proficiency int   `json:"proficiency"`
}

// SkillFormOption is a minimal id+name row for tag/department pickers.
type SkillFormOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// SkillRoutingUser is returned by SkillRoutingService.FindMatchingAgents.
type SkillRoutingUser struct {
	ID    int64  `json:"id"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}
