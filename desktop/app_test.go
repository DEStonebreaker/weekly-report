package main

import (
	"testing"
	"time"

	"weekly-report/internal/app"
)

func TestManualRangePresets(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.Local) // Tuesday
	for _, test := range []struct {
		preset, from, to string
		start, end       string
	}{
		{"next7", "", "", "2026-09-22", "2026-09-28"},
		{"next14", "", "", "2026-09-22", "2026-10-05"},
		{"thisWeek", "", "", "2026-09-21", "2026-09-27"},
		{"nextWeek", "", "", "2026-09-28", "2026-10-04"},
		{"custom", "2026-09-01", "2026-09-03", "2026-09-01", "2026-09-03"},
	} {
		t.Run(test.preset, func(t *testing.T) {
			options, err := manualRange(test.preset, test.from, test.to)
			if err != nil {
				t.Fatal(err)
			}
			window, err := options.Resolve(now)
			if err != nil {
				t.Fatal(err)
			}
			if window.Start.Format("2006-01-02") != test.start || window.End.Format("2006-01-02") != test.end {
				t.Fatalf("window %s–%s, want %s–%s", window.Start.Format("2006-01-02"), window.End.Format("2006-01-02"), test.start, test.end)
			}
		})
	}
}

func TestStartupCatchupRequiresAnActiveWindowAndPastScheduledTime(t *testing.T) {
	settings := app.Config{ScheduleEnabled: true, ScheduleTime: "09:00", StartDate: "2026-09-19", EndDate: "2026-10-05", WindowDays: 7}
	location := time.Local
	for _, test := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{"before trigger", time.Date(2026, 9, 22, 8, 59, 0, 0, location), false},
		{"within active window after trigger", time.Date(2026, 9, 22, 9, 0, 0, 0, location), true},
		{"expired schedule window", time.Date(2026, 10, 6, 10, 0, 0, 0, location), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldCatchUp(settings, test.now); got != test.want {
				t.Fatalf("shouldCatchUp(%s) = %v, want %v", test.now, got, test.want)
			}
		})
	}
	settings.ScheduleEnabled = false
	if shouldCatchUp(settings, time.Date(2026, 9, 22, 10, 0, 0, 0, location)) {
		t.Fatal("disabled schedule caught up")
	}
}

func TestManualRangeRejectsInvalidCustomDates(t *testing.T) {
	for _, test := range []struct{ from, to string }{{"2026-09-01", ""}, {"2026-09-03", "2026-09-01"}, {"bad", "2026-09-03"}} {
		if _, err := manualRange("custom", test.from, test.to); err == nil {
			t.Errorf("custom range %q through %q was accepted", test.from, test.to)
		}
	}
	if _, err := manualRange("unknown", "", ""); err == nil {
		t.Fatal("unknown preset was accepted")
	}
}
