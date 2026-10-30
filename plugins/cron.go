package plugins

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Five-field cron expressions (minute hour day-of-month month day-of-week),
// as the plugin SDK's cron keys are written, evaluated in UTC. Supports *,
// numbers, a-b ranges, /step, comma lists, month and weekday names, 7 as
// Sunday, and the @hourly-style shorthands. When both day fields are
// restricted, either one matching is enough (as cron does).

var cronShorthands = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

type cronField struct {
	min, max int
	names    []string
	base     int
}

var cronFields = []cronField{
	{min: 0, max: 59},
	{min: 0, max: 23},
	{min: 1, max: 31},
	{min: 1, max: 12, names: []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}, base: 1},
	{min: 0, max: 7, names: []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}},
}

func (f cronField) value(token string) (int, bool) {
	for i, name := range f.names {
		if strings.EqualFold(token, name) {
			return i + f.base, true
		}
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(token)
	if err != nil || n < f.min || n > f.max {
		return 0, false
	}
	return n, true
}

// parse returns the values the field allows and whether it was a bare *.
func (f cronField) parse(text string) (map[int]bool, bool, bool) {
	values := map[int]bool{}
	anyValue := false
	for _, part := range strings.Split(text, ",") {
		pieces := strings.Split(part, "/")
		if len(pieces) > 2 {
			return nil, false, false
		}
		step := 1
		if len(pieces) == 2 {
			n, ok := cronField{min: 1, max: 1 << 30}.value(pieces[1])
			if !ok {
				return nil, false, false
			}
			step = n
		}
		rng := pieces[0]
		var from, to int
		switch {
		case rng == "*":
			from, to = f.min, f.max
			if len(pieces) == 1 {
				anyValue = true
			}
		case strings.Contains(rng, "-"):
			ends := strings.Split(rng, "-")
			if len(ends) != 2 {
				return nil, false, false
			}
			a, okA := f.value(ends[0])
			b, okB := f.value(ends[1])
			if !okA || !okB || a > b {
				return nil, false, false
			}
			from, to = a, b
		default:
			n, ok := f.value(rng)
			if !ok {
				return nil, false, false
			}
			from, to = n, n
			if len(pieces) == 2 {
				to = f.max
			}
		}
		for n := from; n <= to; n += step {
			values[n] = true
		}
	}
	return values, anyValue, true
}

// cronDue reports whether expr is due in the minute containing at (UTC).
func cronDue(expr string, at time.Time) (bool, error) {
	text := strings.TrimSpace(expr)
	if short, ok := cronShorthands[strings.ToLower(text)]; ok {
		text = short
	}
	parts := strings.Fields(text)
	if len(parts) != 5 {
		return false, fmt.Errorf("invalid cron expression %q", expr)
	}
	values := make([]map[int]bool, 5)
	anyValue := make([]bool, 5)
	for i, part := range parts {
		v, a, ok := cronFields[i].parse(part)
		if !ok {
			return false, fmt.Errorf("invalid cron expression %q", expr)
		}
		values[i], anyValue[i] = v, a
	}
	if values[4][7] {
		values[4][0] = true
	}
	at = at.UTC()
	if !values[0][at.Minute()] || !values[1][at.Hour()] || !values[3][int(at.Month())] {
		return false, nil
	}
	dom, dow := values[2][at.Day()], values[4][int(at.Weekday())]
	if !anyValue[2] && !anyValue[4] {
		return dom || dow, nil
	}
	return dom && dow, nil
}

// RunDueCron runs, side by side, every cron schedule of every active plugin
// that is due in the minute of now (UTC): an SDK plugin's through the
// runtime's cron method, a native plugin's (CronPlugin) in process. Each runs
// as no user and within 30 s. A failing, panicking or unreadable schedule is
// logged and never stops the others. Call it once a minute from your
// scheduler, or use Escalated.StartPluginCron.
func (h *Host) RunDueCron(ctx context.Context, now time.Time) {
	if h == nil || !h.hasPlugins() {
		return
	}
	active, err := h.activeEntries(ctx)
	if err != nil {
		h.logf("cron: %v", err)
		return
	}
	// The calls carry no request, so plugin writes act as nobody.
	var wg sync.WaitGroup
	for _, e := range active {
		if e.sdk && h.bridge == nil {
			continue
		}
		for _, schedule := range e.cron {
			due, err := cronDue(schedule, now)
			if err != nil {
				h.logf("plugin %s cron %q skipped: %v", e.name, schedule, err)
				continue
			}
			if !due {
				continue
			}
			wg.Add(1)
			if !e.sdk {
				go func(e *entry, schedule string, fn CronFunc) {
					defer wg.Done()
					if err := h.runNative(ctx, actionTimeout, func(c context.Context) error { return fn(c, h.context(e, nil)) }); err != nil {
						h.logf("plugin %s cron %q failed: %v", e.name, schedule, err)
					}
				}(e, schedule, e.cronFuncs[schedule])
				continue
			}
			go func(name, schedule string) {
				defer wg.Done()
				if _, err := h.bridge.call(ctx, scope{plugins: []string{name}}, "cron", map[string]any{"plugin": name, "schedule": schedule}, actionTimeout); err != nil {
					h.logf("plugin %s cron %q failed: %v", name, schedule, err)
				}
			}(e.name, schedule)
		}
	}
	wg.Wait()
}
