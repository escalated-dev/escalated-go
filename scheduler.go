package escalated

import (
	"context"
	"log"
	"time"

	"github.com/escalated-dev/escalated-go/services"
)

// SchedulingConfig controls the package's own runner for the recurring jobs.
type SchedulingConfig struct {
	// AutoStart makes New start StartScheduler in the background (Close
	// stops it), so the host has nothing to schedule. Off by default: a host
	// that runs the jobs from its own scheduler, or on one instance of
	// several, keeps doing so.
	AutoStart bool
}

// schedulerWarnEvery is how often SchedulerStatus logs that the recurring
// jobs are not running.
const schedulerWarnEvery = 15 * time.Minute

// RunEscalations evaluates the active escalation rules against open tickets
// (Laravel's escalated:evaluate-escalations). Call it every five minutes, or
// use StartScheduler.
func (e *Escalated) RunEscalations(_ context.Context) (int, error) {
	svc := services.NewEscalationService(e.Config.DB, nil, e.Access)
	svc.Tickets = e.NewTicketService()
	return svc.EvaluateRules()
}

// RunAutomations runs the active time-based automations (Laravel's
// escalated:run-automations). Call it every five minutes, or use
// StartScheduler.
func (e *Escalated) RunAutomations(_ context.Context) (int, error) {
	runner := services.NewAutomationRunner(e.Config.DB, nil, e.Access)
	runner.Tickets = e.NewTicketService()
	return runner.Run()
}

// WakeSnoozedTickets reopens snoozed tickets whose snooze has run out
// (Laravel's escalated:wake-snoozed-tickets). Call it every minute, or use
// StartScheduler.
func (e *Escalated) WakeSnoozedTickets(ctx context.Context) (int, error) {
	return services.NewSnoozeService(e.Store).WakeExpiredSnoozes(ctx)
}

// StartScheduler runs the recurring jobs (services.RecurringJobs) until ctx
// is done or the returned function is called: the SLA check and snooze
// wake-ups every minute, escalation rules and automations every five
// minutes. Every job runs once straight away. Errors are logged and the next
// run goes ahead as usual. Run it on one instance only.
//
// Config.Scheduling.AutoStart has New start it for you.
func (e *Escalated) StartScheduler(ctx context.Context) func() {
	return e.startScheduler(ctx, time.Minute)
}

func (e *Escalated) startScheduler(ctx context.Context, tick time.Duration) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for n := 0; ; n++ {
			e.runRecurringJobs(ctx, n%5 == 0)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (e *Escalated) runRecurringJobs(ctx context.Context, fiveMinute bool) {
	if ctx.Err() != nil {
		return
	}
	if _, _, err := e.CheckSLA(ctx, 0); err != nil {
		log.Printf("escalated: SLA check: %v", err)
	}
	if _, err := e.WakeSnoozedTickets(ctx); err != nil {
		log.Printf("escalated: snooze wake-up: %v", err)
	}
	if !fiveMinute {
		return
	}
	if _, err := e.RunEscalations(ctx); err != nil {
		log.Printf("escalated: escalation rules: %v", err)
	}
	if _, err := e.RunAutomations(ctx); err != nil {
		log.Printf("escalated: automations: %v", err)
	}
}

// SchedulerStatus reports whether the recurring jobs are running, from the
// heartbeat CheckSLA records: unhealthy when it is missing or older than 15
// minutes. The admin Settings, Reports and report dashboard pages show it as the
// `scheduler` prop; a host can serve it from its own health check. Nil when
// the settings cannot be read. While unhealthy it also logs a warning, at
// most every 15 minutes.
func (e *Escalated) SchedulerStatus(ctx context.Context) *services.SchedulerStatus {
	if e.Store == nil {
		return nil
	}
	now := time.Now()
	status, err := services.ReadSchedulerStatus(ctx, e.Store, now)
	if err != nil {
		return nil
	}
	if !status.Healthy {
		last := e.schedulerWarnedAt.Load()
		if (last == 0 || now.Sub(time.Unix(0, last)) >= schedulerWarnEvery) &&
			e.schedulerWarnedAt.CompareAndSwap(last, now.UnixNano()) {
			log.Printf("escalated: %s", status.Message)
		}
	}
	return status
}

// autoStartScheduler starts the scheduler when Config.Scheduling.AutoStart is
// set; Close stops it.
func autoStartScheduler(e *Escalated) *Escalated {
	if e.Config.Scheduling.AutoStart {
		e.stopScheduler = e.StartScheduler(context.Background())
	}
	return e
}
