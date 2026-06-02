package services

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// SkillRoutingService resolves agents whose escalated_agent_skills rows
// cover every skill required by explicit tag/department routing rules.
// See escalated-developer-context/decisions/2026-05-13-skills-routing-explicit-mapping.md.
type SkillRoutingService struct {
	db     *sql.DB
	prefix string
}

// NewSkillRoutingService constructs a routing service. tablePrefix is
// typically "escalated_" so queries hit escalated_skills, etc.
func NewSkillRoutingService(db *sql.DB, tablePrefix string) *SkillRoutingService {
	return &SkillRoutingService{db: db, prefix: tablePrefix}
}

func (s *SkillRoutingService) t(name string) string {
	return s.prefix + name
}

// FindMatchingAgents returns users who have proficiency rows for every
// required skill (union of skills matched by ticket tags and by ticket
// department), ordered by sum(proficiency) desc then open ticket load asc.
func (s *SkillRoutingService) FindMatchingAgents(ctx context.Context, ticket *models.Ticket) ([]models.SkillRoutingUser, error) {
	if ticket == nil || ticket.ID == 0 {
		return nil, fmt.Errorf("skill routing: ticket required")
	}

	required, err := s.requiredSkillIDs(ctx, ticket)
	if err != nil {
		return nil, err
	}
	if len(required) == 0 {
		return nil, nil
	}

	candidates, err := s.candidatesWithAllSkills(ctx, required)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	userIDs := make([]models.UserID, 0, len(candidates))
	for uid := range candidates {
		userIDs = append(userIDs, uid)
	}
	loads, err := s.openTicketLoads(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	type scored struct {
		id        models.UserID
		sumProf   int
		ticketCnt int
	}
	out := make([]scored, 0, len(candidates))
	for uid, sumProf := range candidates {
		out = append(out, scored{
			id:        uid,
			sumProf:   sumProf,
			ticketCnt: loads[uid],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].sumProf != out[j].sumProf {
			return out[i].sumProf > out[j].sumProf
		}
		if out[i].ticketCnt != out[j].ticketCnt {
			return out[i].ticketCnt < out[j].ticketCnt
		}
		return out[i].id < out[j].id
	})

	users := make([]models.SkillRoutingUser, 0, len(out))
	for _, row := range out {
		users = append(users, models.SkillRoutingUser{ID: row.id})
	}
	return users, nil
}

func (s *SkillRoutingService) tagIDsForTicket(ctx context.Context, ticket *models.Ticket) ([]int64, error) {
	if len(ticket.Tags) > 0 {
		out := make([]int64, 0, len(ticket.Tags))
		for _, t := range ticket.Tags {
			out = append(out, t.ID)
		}
		return out, nil
	}
	rows, err := s.db.QueryContext(
		ctx,
		fmt.Sprintf(`SELECT tag_id FROM %s WHERE ticket_id = ? ORDER BY tag_id`, s.t("ticket_tags")),
		ticket.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var tid int64
		if err := rows.Scan(&tid); err != nil {
			return nil, err
		}
		out = append(out, tid)
	}
	return out, rows.Err()
}

func (s *SkillRoutingService) requiredSkillIDs(ctx context.Context, ticket *models.Ticket) ([]int64, error) {
	seen := make(map[int64]struct{})

	tagIDs, err := s.tagIDsForTicket(ctx, ticket)
	if err != nil {
		return nil, err
	}
	if len(tagIDs) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(tagIDs)), ",")
		args := make([]any, len(tagIDs))
		for i, id := range tagIDs {
			args[i] = id
		}
		q := fmt.Sprintf(
			`SELECT DISTINCT skill_id FROM %s WHERE tag_id IN (%s)`,
			s.t("skill_routing_tags"), ph,
		)
		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sid int64
			if err := rows.Scan(&sid); err != nil {
				rows.Close()
				return nil, err
			}
			seen[sid] = struct{}{}
		}
		rows.Close()
	}

	if ticket.DepartmentID != nil {
		rows, err := s.db.QueryContext(
			ctx,
			fmt.Sprintf(`SELECT DISTINCT skill_id FROM %s WHERE department_id = ?`, s.t("skill_routing_departments")),
			*ticket.DepartmentID,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sid int64
			if err := rows.Scan(&sid); err != nil {
				rows.Close()
				return nil, err
			}
			seen[sid] = struct{}{}
		}
		rows.Close()
	}

	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (s *SkillRoutingService) candidatesWithAllSkills(ctx context.Context, required []int64) (map[models.UserID]int, error) {
	n := len(required)
	ph := strings.TrimSuffix(strings.Repeat("?,", n), ",")
	args := make([]any, n+1)
	for i, id := range required {
		args[i] = id
	}
	args[n] = n

	q := fmt.Sprintf(
		`SELECT user_id, SUM(proficiency) AS sum_prof
		   FROM %s
		  WHERE skill_id IN (%s)
		  GROUP BY user_id
		 HAVING COUNT(DISTINCT skill_id) = ?`,
		s.t("agent_skills"), ph,
	)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[models.UserID]int)
	for rows.Next() {
		var uid models.UserID
		var sumProf int
		if err := rows.Scan(&uid, &sumProf); err != nil {
			return nil, err
		}
		out[uid] = sumProf
	}
	return out, rows.Err()
}

func (s *SkillRoutingService) openTicketLoads(ctx context.Context, userIDs []models.UserID) (map[models.UserID]int, error) {
	out := make(map[models.UserID]int)
	if len(userIDs) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(userIDs)), ",")
	args := make([]any, len(userIDs)+2)
	for i, id := range userIDs {
		args[i] = id
	}
	args[len(userIDs)] = models.StatusResolved
	args[len(userIDs)+1] = models.StatusClosed

	q := fmt.Sprintf(
		`SELECT assigned_to, COUNT(*) FROM %s
		  WHERE assigned_to IN (%s) AND status NOT IN (?, ?)
		  GROUP BY assigned_to`,
		s.t("tickets"), ph,
	)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid models.UserID
		var cnt int
		if err := rows.Scan(&uid, &cnt); err != nil {
			return nil, err
		}
		out[uid] = cnt
	}
	return out, rows.Err()
}
