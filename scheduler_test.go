package escalated

import (
	"context"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/services"
)

func newSchedulerTestInstance(t *testing.T, configure ...func(*Config)) *Escalated {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DB = testdb.Open(t)
	for _, c := range configure {
		c(&cfg)
	}
	esc, err := New(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(esc.Close)
	return esc
}

func TestCheckSLARecordsTheSchedulerHeartbeat(t *testing.T) {
	esc := newSchedulerTestInstance(t)
	ctx := context.Background()

	before := esc.SchedulerStatus(ctx)
	if before == nil || before.Healthy || before.LastRunAt != nil {
		t.Fatalf("before any SLA check: %+v", before)
	}
	if len(before.Commands) == 0 {
		t.Fatal("an unhealthy status must name the jobs to schedule")
	}

	if _, _, err := esc.CheckSLA(ctx, 0); err != nil {
		t.Fatalf("check sla: %v", err)
	}
	raw, err := esc.Store.GetSetting(ctx, services.SchedulerHeartbeatSetting)
	if err != nil || raw == "" {
		t.Fatalf("heartbeat %q, %v", raw, err)
	}
	after := esc.SchedulerStatus(ctx)
	if after == nil || !after.Healthy || after.LastRunAt == nil {
		t.Fatalf("after an SLA check: %+v", after)
	}

	// Sixteen minutes without a run: unhealthy again.
	stale := time.Now().Add(-16 * time.Minute).UTC().Format(time.RFC3339)
	if err := esc.Store.SetSetting(ctx, services.SchedulerHeartbeatSetting, stale); err != nil {
		t.Fatal(err)
	}
	if got := esc.SchedulerStatus(ctx); got == nil || got.Healthy {
		t.Fatalf("stale heartbeat: %+v", got)
	}
}

func TestStartSchedulerRunsTheRecurringJobs(t *testing.T) {
	esc := newSchedulerTestInstance(t)
	ctx := context.Background()

	stop := esc.startScheduler(ctx, 10*time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if status := esc.SchedulerStatus(ctx); status != nil && status.Healthy {
			break
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatal("the scheduler never ran the SLA check")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
}

func TestSchedulingAutoStartIsOptIn(t *testing.T) {
	off := newSchedulerTestInstance(t)
	if off.stopScheduler != nil {
		t.Fatal("the scheduler started without Config.Scheduling.AutoStart")
	}

	on := newSchedulerTestInstance(t, func(c *Config) { c.Scheduling.AutoStart = true })
	if on.stopScheduler == nil {
		t.Fatal("Config.Scheduling.AutoStart did not start the scheduler")
	}
	// Every job runs once straight away, so the heartbeat appears.
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if status := on.SchedulerStatus(ctx); status != nil && status.Healthy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the auto-started scheduler never ran the SLA check")
		}
		time.Sleep(10 * time.Millisecond)
	}
	on.Close()
	if on.stopScheduler != nil {
		t.Fatal("Close left the scheduler running")
	}
}
