package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
)

// SkillAgentDirectory lets the host supply agents for the Skills form
// pickers. Optional — when nil, available_agents is empty and agent
// user_ids are not cross-checked against a directory.
type SkillAgentDirectory interface {
	ListAgentsForSkillForm(ctx context.Context) ([]SkillFormAgent, error)
}

// SkillFormAgent is one row for available_agents on the Skills form.
type SkillFormAgent struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// SkillsHandler serves admin Skills CRUD per domain-model/skills-management.md.
type SkillsHandler struct {
	DB       *sql.DB
	Prefix   string
	Renderer renderer.Renderer
	Agents   SkillAgentDirectory
}

// NewSkillsHandler constructs a handler. prefix is the table prefix (e.g. escalated_).
func NewSkillsHandler(db *sql.DB, prefix string, rend renderer.Renderer, agents SkillAgentDirectory) *SkillsHandler {
	if prefix == "" {
		prefix = "escalated_"
	}
	return &SkillsHandler{DB: db, Prefix: prefix, Renderer: rend, Agents: agents}
}

func (h *SkillsHandler) t(name string) string {
	return h.Prefix + name
}

// ListSkills handles GET /admin/skills — index props.
func (h *SkillsHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(
		r.Context(),
		fmt.Sprintf(`SELECT s.id, s.name, s.updated_at,
			(SELECT COUNT(*) FROM %s a WHERE a.skill_id = s.id),
			(SELECT COUNT(*) FROM %s rt WHERE rt.skill_id = s.id),
			(SELECT COUNT(*) FROM %s rd WHERE rd.skill_id = s.id)
			FROM %s s ORDER BY s.name ASC`,
			h.t("agent_skills"), h.t("skill_routing_tags"), h.t("skill_routing_departments"), h.t("skills")),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var skills []models.SkillListItem
	for rows.Next() {
		var it models.SkillListItem
		if err := rows.Scan(&it.ID, &it.Name, &it.UpdatedAt, &it.AgentsCount, &it.RoutingTagsCount, &it.RoutingDepartmentsCount); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		skills = append(skills, it)
	}
	_ = h.Renderer.Render(w, r, "Admin/Skills/Index", map[string]any{
		"skills": skills,
	})
}

// NewSkillForm handles GET /admin/skills/new.
func (h *SkillsHandler) NewSkillForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tags, depts, agents, err := h.loadFormLists(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.Renderer.Render(w, r, "Admin/Skills/Form", map[string]any{
		"skill":                 nil,
		"available_tags":        tags,
		"available_departments": depts,
		"available_agents":      agents,
	})
}

// EditSkill handles GET /admin/skills/{id}/edit.
func (h *SkillsHandler) EditSkill(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPathName(r, "id")
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	detail, err := h.loadSkillFormPayload(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tags, depts, agents, err := h.loadFormLists(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.Renderer.Render(w, r, "Admin/Skills/Form", map[string]any{
		"skill":                 detail,
		"available_tags":        tags,
		"available_departments": depts,
		"available_agents":      agents,
	})
}

type skillWriteBody struct {
	Name                 string                `json:"name"`
	RoutingTagIDs        []int64               `json:"routing_tag_ids"`
	RoutingDepartmentIDs []int64               `json:"routing_department_ids"`
	Agents               []skillWriteBodyAgent `json:"agents"`
}

type skillWriteBodyAgent struct {
	UserID      int64 `json:"user_id"`
	Proficiency *int  `json:"proficiency"`
}

// StoreSkill handles POST /admin/skills.
func (h *SkillsHandler) StoreSkill(w http.ResponseWriter, r *http.Request) {
	var in skillWriteBody
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := h.validateWrite(r.Context(), in, 0); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now()
	name := strings.TrimSpace(in.Name)
	slug, err := h.allocateSlug(tx, r.Context(), name, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	res, err := tx.ExecContext(
		r.Context(),
		fmt.Sprintf(`INSERT INTO %s (name, slug, description, created_at, updated_at) VALUES (?, ?, NULL, ?, ?)`, h.t("skills")),
		name, slug, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			http.Error(w, "name or slug already taken", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	skillID, err := res.LastInsertId()
	if err != nil || skillID == 0 {
		http.Error(w, "could not read skill id", http.StatusInternalServerError)
		return
	}

	if err := h.syncRelationsTx(tx, r.Context(), skillID, in); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": skillID})
}

// UpdateSkill handles PUT/PATCH /admin/skills/{id}.
func (h *SkillsHandler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPathName(r, "id")
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var in skillWriteBody
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := h.validateWrite(r.Context(), in, id); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	var curSlug string
	row := tx.QueryRowContext(r.Context(), fmt.Sprintf(`SELECT slug FROM %s WHERE id = ?`, h.t("skills")), id)
	if err := row.Scan(&curSlug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	name := strings.TrimSpace(in.Name)
	_ = curSlug // kept for future no-op cases; allocateSlug currently always returns one
	var slug string
	if ns, err := h.allocateSlug(tx, r.Context(), name, id); err == nil {
		slug = ns
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := tx.ExecContext(
		r.Context(),
		fmt.Sprintf(`UPDATE %s SET name = ?, slug = ?, updated_at = ? WHERE id = ?`, h.t("skills")),
		name, slug, now, id,
	); err != nil {
		if isUniqueViolation(err) {
			http.Error(w, "name or slug already taken", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.syncRelationsTx(tx, r.Context(), id, in); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// DestroySkill handles DELETE /admin/skills/{id}.
func (h *SkillsHandler) DestroySkill(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPathName(r, "id")
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	res, err := h.DB.ExecContext(r.Context(), fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, h.t("skills")), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SkillsHandler) validateWrite(ctx context.Context, in skillWriteBody, excludeSkillID int64) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > 100 {
		return errors.New("name must be at most 100 characters")
	}
	if taken, err := h.nameTaken(ctx, name, excludeSkillID); err != nil {
		return err
	} else if taken {
		return errors.New("name is already in use")
	}

	if err := h.idsExist(ctx, h.t("tags"), in.RoutingTagIDs); err != nil {
		return err
	}
	if err := h.idsExist(ctx, h.t("departments"), in.RoutingDepartmentIDs); err != nil {
		return err
	}

	seenUser := make(map[int64]struct{})
	for _, a := range in.Agents {
		if a.UserID <= 0 {
			return errors.New("each agent requires a valid user_id")
		}
		if _, dup := seenUser[a.UserID]; dup {
			return errors.New("duplicate user_id in agents")
		}
		seenUser[a.UserID] = struct{}{}
		prof := 3
		if a.Proficiency != nil {
			prof = *a.Proficiency
		}
		if prof < 1 || prof > 5 {
			return errors.New("proficiency must be between 1 and 5")
		}
	}

	if h.Agents != nil {
		list, err := h.Agents.ListAgentsForSkillForm(ctx)
		if err != nil {
			return err
		}
		allowed := make(map[int64]struct{}, len(list))
		for _, u := range list {
			allowed[u.ID] = struct{}{}
		}
		for uid := range seenUser {
			if _, ok := allowed[uid]; !ok {
				return fmt.Errorf("user_id %d is not an available agent", uid)
			}
		}
	}
	return nil
}

func (h *SkillsHandler) nameTaken(ctx context.Context, name string, excludeID int64) (bool, error) {
	var row *sql.Row
	if excludeID == 0 {
		row = h.DB.QueryRowContext(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE name = ? LIMIT 1`, h.t("skills")), name)
	} else {
		row = h.DB.QueryRowContext(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE name = ? AND id <> ? LIMIT 1`, h.t("skills")), name, excludeID)
	}
	var id int64
	switch err := row.Scan(&id); err {
	case nil:
		return true, nil
	case sql.ErrNoRows:
		return false, nil
	default:
		return false, err
	}
}

func (h *SkillsHandler) idsExist(ctx context.Context, table string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id IN (%s)`, table, ph)
	var cnt int
	if err := h.DB.QueryRowContext(ctx, q, args...).Scan(&cnt); err != nil {
		return err
	}
	if cnt != len(ids) {
		return errors.New("one or more referenced ids do not exist")
	}
	return nil
}

func (h *SkillsHandler) allocateSlug(tx *sql.Tx, ctx context.Context, baseName string, excludeID int64) (string, error) {
	base := slugify(baseName)
	if base == "" {
		base = "skill"
	}
	for i := 0; i < 50; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		var row *sql.Row
		if excludeID == 0 {
			row = tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE slug = ? LIMIT 1`, h.t("skills")), candidate)
		} else {
			row = tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE slug = ? AND id <> ? LIMIT 1`, h.t("skills")), candidate, excludeID)
		}
		var id int64
		switch err := row.Scan(&id); err {
		case sql.ErrNoRows:
			return candidate, nil
		case nil:
			continue
		default:
			return "", err
		}
	}
	return "", errors.New("could not allocate unique slug")
}

func (h *SkillsHandler) syncRelationsTx(tx *sql.Tx, ctx context.Context, skillID int64, in skillWriteBody) error {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE skill_id = ?`, h.t("skill_routing_tags")), skillID); err != nil {
		return err
	}
	for _, tid := range in.RoutingTagIDs {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (skill_id, tag_id) VALUES (?, ?)`, h.t("skill_routing_tags")), skillID, tid); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE skill_id = ?`, h.t("skill_routing_departments")), skillID); err != nil {
		return err
	}
	for _, did := range in.RoutingDepartmentIDs {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (skill_id, department_id) VALUES (?, ?)`, h.t("skill_routing_departments")), skillID, did); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE skill_id = ?`, h.t("agent_skills")), skillID); err != nil {
		return err
	}
	now := time.Now()
	for _, a := range in.Agents {
		prof := 3
		if a.Proficiency != nil {
			prof = *a.Proficiency
		}
		if _, err := tx.ExecContext(
			ctx,
			fmt.Sprintf(`INSERT INTO %s (user_id, skill_id, proficiency, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, h.t("agent_skills")),
			a.UserID, skillID, prof, now, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func (h *SkillsHandler) loadFormLists(ctx context.Context) ([]models.SkillFormOption, []models.SkillFormOption, []SkillFormAgent, error) {
	var tags []models.SkillFormOption
	rows, err := h.DB.QueryContext(ctx, fmt.Sprintf(`SELECT id, name FROM %s ORDER BY name ASC`, h.t("tags")))
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var o models.SkillFormOption
		if err := rows.Scan(&o.ID, &o.Name); err != nil {
			return nil, nil, nil, err
		}
		tags = append(tags, o)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	var depts []models.SkillFormOption
	rows2, err := h.DB.QueryContext(ctx, fmt.Sprintf(`SELECT id, name FROM %s ORDER BY name ASC`, h.t("departments")))
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var o models.SkillFormOption
		if err := rows2.Scan(&o.ID, &o.Name); err != nil {
			return nil, nil, nil, err
		}
		depts = append(depts, o)
	}
	if err := rows2.Err(); err != nil {
		return nil, nil, nil, err
	}

	var agents []SkillFormAgent
	if h.Agents != nil {
		agents, err = h.Agents.ListAgentsForSkillForm(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return tags, depts, agents, nil
}

func (h *SkillsHandler) loadSkillFormPayload(ctx context.Context, id int64) (*models.SkillFormPayload, error) {
	var name string
	err := h.DB.QueryRowContext(ctx, fmt.Sprintf(`SELECT name FROM %s WHERE id = ?`, h.t("skills")), id).Scan(&name)
	if err != nil {
		return nil, err
	}
	out := &models.SkillFormPayload{
		ID:                   id,
		Name:                 name,
		RoutingTagIDs:        nil,
		RoutingDepartmentIDs: nil,
		Agents:               nil,
	}

	rtRows, err := h.DB.QueryContext(ctx, fmt.Sprintf(`SELECT tag_id FROM %s WHERE skill_id = ? ORDER BY tag_id`, h.t("skill_routing_tags")), id)
	if err != nil {
		return nil, err
	}
	defer rtRows.Close()
	for rtRows.Next() {
		var tid int64
		if err := rtRows.Scan(&tid); err != nil {
			return nil, err
		}
		out.RoutingTagIDs = append(out.RoutingTagIDs, tid)
	}
	if err := rtRows.Err(); err != nil {
		return nil, err
	}

	rdRows, err := h.DB.QueryContext(ctx, fmt.Sprintf(`SELECT department_id FROM %s WHERE skill_id = ? ORDER BY department_id`, h.t("skill_routing_departments")), id)
	if err != nil {
		return nil, err
	}
	defer rdRows.Close()
	for rdRows.Next() {
		var did int64
		if err := rdRows.Scan(&did); err != nil {
			return nil, err
		}
		out.RoutingDepartmentIDs = append(out.RoutingDepartmentIDs, did)
	}
	if err := rdRows.Err(); err != nil {
		return nil, err
	}

	rows3, err := h.DB.QueryContext(ctx, fmt.Sprintf(`SELECT user_id, proficiency FROM %s WHERE skill_id = ? ORDER BY user_id`, h.t("agent_skills")), id)
	if err != nil {
		return nil, err
	}
	defer rows3.Close()
	for rows3.Next() {
		var uid int64
		var prof int
		if err := rows3.Scan(&uid, &prof); err != nil {
			return nil, err
		}
		out.Agents = append(out.Agents, models.SkillFormAgentRow{UserID: uid, Proficiency: prof})
	}
	return out, rows3.Err()
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") ||
		strings.Contains(s, "duplicate key value violates unique constraint") ||
		strings.Contains(s, "UNIQUE constraint violation")
}
