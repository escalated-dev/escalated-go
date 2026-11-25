package services

import (
	"context"
	"fmt"
	"time"

	"github.com/escalated-dev/escalated-go/store"
)

// SchedulerHeartbeatSetting is the settings key the SLA check writes its last
// run time to (RFC 3339, UTC). The admin pages read it to tell whether the
// recurring jobs are being run.
const SchedulerHeartbeatSetting = "scheduler_heartbeat_at"

// SchedulerStaleAfter is how old the heartbeat may be before the recurring
// jobs count as not running. The SLA check runs every minute, so fifteen
// missed runs in a row are not a slow tick.
const SchedulerStaleAfter = 15 * time.Minute

// ScheduledJob is one recurring job the host must run, and how often.
type ScheduledJob struct {
	Command  string `json:"command"`
	Interval string `json:"interval"`
}

// RecurringJobs are the jobs Escalated needs run on a schedule. Escalated.
// StartScheduler runs all of them; a host with its own scheduler calls each
// one at its interval.
var RecurringJobs = []ScheduledJob{
	{Command: "esc.CheckSLA(ctx, 0)", Interval: "every minute"},
	{Command: "esc.WakeSnoozedTickets(ctx)", Interval: "every minute"},
	{Command: "esc.RunEscalations(ctx)", Interval: "every 5 minutes"},
	{Command: "esc.RunAutomations(ctx)", Interval: "every 5 minutes"},
}

// SchedulerStatus is the `scheduler` prop on the admin Settings and report
// dashboard pages: whether the recurring jobs ran recently, when the SLA check
// last ran, a message for admins and the jobs to schedule.
type SchedulerStatus struct {
	Healthy   bool           `json:"healthy"`
	LastRunAt *string        `json:"last_run_at"`
	Message   string         `json:"message"`
	Commands  []ScheduledJob `json:"commands"`
}

// RecordSchedulerHeartbeat stores now as the SLA check's last run.
func RecordSchedulerHeartbeat(ctx context.Context, s store.Store, now time.Time) error {
	return s.SetSetting(ctx, SchedulerHeartbeatSetting, now.UTC().Format(time.RFC3339))
}

// ReadSchedulerStatus reads the heartbeat and reports, as of now, whether the
// recurring jobs are running. The status is unhealthy when the heartbeat is
// missing, unreadable or older than SchedulerStaleAfter. An error means the
// settings could not be read, so nothing can be told.
func ReadSchedulerStatus(ctx context.Context, s store.Store, now time.Time) (*SchedulerStatus, error) {
	raw, err := s.GetSetting(ctx, SchedulerHeartbeatSetting)
	if err != nil {
		return nil, err
	}
	status := &SchedulerStatus{Commands: append([]ScheduledJob(nil), RecurringJobs...)}
	last, parseErr := time.Parse(time.RFC3339, raw)
	if raw == "" || parseErr != nil {
		status.Message = "Escalated's recurring jobs are not running: the SLA check has never run, so SLA breaches, escalation rules, automations and snooze wake-ups are not processed. " + schedulerHowTo
		return status, nil
	}
	at := last.UTC().Format(time.RFC3339)
	status.LastRunAt = &at
	if now.Sub(last) > SchedulerStaleAfter {
		status.Message = fmt.Sprintf("Escalated's recurring jobs are not running: the SLA check last ran at %s, more than %d minutes ago, so SLA breaches, escalation rules, automations and snooze wake-ups are not processed. %s", at, int(SchedulerStaleAfter/time.Minute), schedulerHowTo)
		return status, nil
	}
	status.Healthy = true
	status.Message = "Escalated's recurring jobs are running."
	return status, nil
}

const schedulerHowTo = "Call esc.StartScheduler(ctx) once at startup (or set Config.Scheduling.AutoStart), or run each job below from your own scheduler."
