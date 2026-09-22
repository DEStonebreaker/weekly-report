package report

import (
	"testing"
	"time"
)

func TestManualRanges(t *testing.T) {
	one, fourteen := 1, 14
	for _, tc := range []struct {
		name, today string
		options     RangeOptions
		start, end  string
	}{
		{"default", "2026-09-22", RangeOptions{}, "2026-09-22", "2026-09-28"},
		{"one day", "2026-09-22", RangeOptions{Days: &one}, "2026-09-22", "2026-09-22"},
		{"fourteen days", "2026-09-22", RangeOptions{Days: &fourteen}, "2026-09-22", "2026-10-05"},
		{"this week", "2026-09-22", RangeOptions{ThisWeek: true}, "2026-09-21", "2026-09-27"},
		{"next week", "2026-09-22", RangeOptions{NextWeek: true}, "2026-09-28", "2026-10-04"},
		{"Sunday", "2026-09-27", RangeOptions{ThisWeek: true}, "2026-09-21", "2026-09-27"},
		{"Monday", "2026-09-28", RangeOptions{ThisWeek: true}, "2026-09-28", "2026-10-04"},
		{"year boundary", "2026-12-31", RangeOptions{NextWeek: true}, "2027-01-04", "2027-01-10"},
		{"past range", "2026-09-22", RangeOptions{From: "2026-08-01", To: "2026-08-03"}, "2026-08-01", "2026-08-03"},
		{"same date", "2026-09-22", RangeOptions{From: "2026-09-25", To: "2026-09-25"}, "2026-09-25", "2026-09-25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			day, _ := time.Parse("2006-01-02", tc.today)
			// Late evening locally is already tomorrow in UTC.
			now := time.Date(day.Year(), day.Month(), day.Day(), 23, 30, 0, 0, time.FixedZone("local", -5*3600))
			got, err := tc.options.Resolve(now)
			if err != nil {
				t.Fatal(err)
			}
			if got.Start.Format("2006-01-02") != tc.start || got.End.Format("2006-01-02") != tc.end {
				t.Fatalf("range=%v, want %s through %s", got, tc.start, tc.end)
			}
		})
	}
}
