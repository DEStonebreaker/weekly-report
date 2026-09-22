package report

import (
	"testing"
	"time"
)

func TestActiveWindow(t *testing.T) {
	config := Schedule{StartDate: "2026-09-19", EndDate: "2026-10-05", WindowDays: 7}
	cases := []struct{ today, start, end string }{
		{"2026-09-18", "", ""},
		{"2026-09-19", "2026-09-19", "2026-09-25"},
		{"2026-09-30", "2026-09-26", "2026-10-02"},
		{"2026-10-05", "2026-10-03", "2026-10-05"},
		{"2026-10-06", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.today, func(t *testing.T) {
			now, _ := time.Parse("2006-01-02", tc.today)
			window, err := ActiveWindow(config, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := window.Start.Format("2006-01-02"); !window.Start.IsZero() && got != tc.start {
				t.Errorf("start=%s want %s", got, tc.start)
			}
			if got := window.End.Format("2006-01-02"); !window.End.IsZero() && got != tc.end {
				t.Errorf("end=%s want %s", got, tc.end)
			}
			if tc.start == "" && !window.Start.IsZero() {
				t.Errorf("expected no active window")
			}
		})
	}
}

func TestActiveWindowUsesLocalCalendarDay(t *testing.T) {
	config := Schedule{StartDate: "2026-12-31", EndDate: "2027-01-02", WindowDays: 2}
	now := time.Date(2027, 1, 1, 23, 30, 0, 0, time.FixedZone("EST", -5*3600))
	window, err := ActiveWindow(config, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := window.Start.Format("2006-01-02"); got != "2026-12-31" {
		t.Errorf("start=%s", got)
	}
}
