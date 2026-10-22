package services

import (
	"context"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// businessScheduleFor picks the schedule a business-hours policy counts
// against for t: the policy's own, else the ticket department's, else the
// default schedule. Nil means calendar hours: the policy is not
// business-hours-only, the store keeps no schedules, or none is configured.
func businessScheduleFor(ctx context.Context, s store.Store, policy *models.SLAPolicy, t *models.Ticket) *models.BusinessSchedule {
	if policy == nil || !policy.BusinessHoursOnly {
		return nil
	}
	repo := store.BusinessSchedulesFor(s)
	if repo == nil {
		return nil
	}
	usable := func(id *int64) *models.BusinessSchedule {
		if id == nil {
			return nil
		}
		sched, err := repo.GetBusinessSchedule(ctx, *id)
		if err != nil || sched == nil || !sched.IsActive {
			return nil
		}
		return sched
	}
	if sched := usable(policy.BusinessScheduleID); sched != nil {
		return sched
	}
	if t != nil && t.DepartmentID != nil {
		if dept, err := s.GetDepartment(ctx, *t.DepartmentID); err == nil && dept != nil {
			if sched := usable(dept.BusinessScheduleID); sched != nil {
				return sched
			}
		}
	}
	sched, err := repo.GetDefaultBusinessSchedule(ctx)
	if err != nil {
		return nil
	}
	return sched
}

// slaDue is base plus hours, in business time when sched is set.
func slaDue(base time.Time, hours float64, sched *models.BusinessSchedule) time.Time {
	if sched == nil {
		return base.Add(time.Duration(hours * float64(time.Hour)))
	}
	return sched.AddBusinessMinutes(base, hours*60)
}

// setSLADeadlines sets the ticket's first-response and resolution deadlines
// from policy, counted from base. A target the policy has no figure for
// leaves the deadline as it is.
func setSLADeadlines(ctx context.Context, s store.Store, policy *models.SLAPolicy, t *models.Ticket, base time.Time) {
	priority := models.PriorityName[t.Priority]
	first, hasFirst := policy.FirstResponseHoursFor(priority)
	resolution, hasResolution := policy.ResolutionHoursFor(priority)
	if !hasFirst && !hasResolution {
		return
	}
	sched := businessScheduleFor(ctx, s, policy, t)
	if hasFirst {
		due := slaDue(base, first, sched)
		t.SLAFirstResponseDueAt = &due
	}
	if hasResolution {
		due := slaDue(base, resolution, sched)
		t.SLAResolutionDueAt = &due
	}
}
