package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// settingsStore is the mock store with a working settings table.
type settingsStore struct {
	*mockStore
	settings map[string]string
	err      error
}

func newSettingsStore() *settingsStore {
	return &settingsStore{mockStore: newMockStore(), settings: map[string]string{}}
}

func (s *settingsStore) GetSetting(_ context.Context, key string) (string, error) {
	return s.settings[key], s.err
}

func (s *settingsStore) SetSetting(_ context.Context, key, value string) error {
	s.settings[key] = value
	return s.err
}

func TestSchedulerStatusWithoutHeartbeatIsUnhealthy(t *testing.T) {
	status, err := ReadSchedulerStatus(context.Background(), newSettingsStore(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if status.Healthy || status.LastRunAt != nil {
		t.Fatalf("no heartbeat: healthy %v last_run_at %v", status.Healthy, status.LastRunAt)
	}
	if !strings.Contains(status.Message, "not running") || !strings.Contains(status.Message, "esc.StartScheduler(ctx)") {
		t.Errorf("message %q does not say what to schedule", status.Message)
	}
	want := map[string]string{
		"esc.CheckSLA(ctx, 0)":        "every minute",
		"esc.WakeSnoozedTickets(ctx)": "every minute",
		"esc.RunEscalations(ctx)":     "every 5 minutes",
		"esc.RunAutomations(ctx)":     "every 5 minutes",
	}
	if len(status.Commands) != len(want) {
		t.Fatalf("commands %v", status.Commands)
	}
	for _, c := range status.Commands {
		if want[c.Command] != c.Interval {
			t.Errorf("command %q interval %q", c.Command, c.Interval)
		}
	}
}

func TestSchedulerHeartbeatMakesStatusHealthyUntilStale(t *testing.T) {
	ctx := context.Background()
	s := newSettingsStore()
	ran := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	if err := RecordSchedulerHeartbeat(ctx, s, ran); err != nil {
		t.Fatal(err)
	}
	if got := s.settings[SchedulerHeartbeatSetting]; got != "2026-11-24T10:00:00Z" {
		t.Fatalf("heartbeat stored as %q", got)
	}

	status, err := ReadSchedulerStatus(ctx, s, ran.Add(14*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Healthy || status.LastRunAt == nil || *status.LastRunAt != "2026-11-24T10:00:00Z" {
		t.Fatalf("fresh heartbeat: %+v", status)
	}

	status, err = ReadSchedulerStatus(ctx, s, ran.Add(16*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Healthy || status.LastRunAt == nil {
		t.Fatalf("16 minute old heartbeat: %+v", status)
	}
	if !strings.Contains(status.Message, "2026-11-24T10:00:00Z") || !strings.Contains(status.Message, "not running") {
		t.Errorf("stale message %q", status.Message)
	}
}

func TestSchedulerStatusUnreadableHeartbeatIsUnhealthy(t *testing.T) {
	s := newSettingsStore()
	s.settings[SchedulerHeartbeatSetting] = "yesterday"
	status, err := ReadSchedulerStatus(context.Background(), s, time.Now())
	if err != nil || status.Healthy {
		t.Fatalf("garbage heartbeat: %+v %v", status, err)
	}
}

func TestSchedulerStatusCannotTellWhenSettingsFail(t *testing.T) {
	s := newSettingsStore()
	s.err = errors.New("db down")
	if status, err := ReadSchedulerStatus(context.Background(), s, time.Now()); err == nil || status != nil {
		t.Fatalf("settings error: %+v %v", status, err)
	}
}
