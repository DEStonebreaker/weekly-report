package report

import (
	"fmt"
	"time"
)

type Schedule struct {
	StartDate  string
	EndDate    string
	WindowDays int
}

func ParseCalendarDate(field, value string) (time.Time, error) {
	date, err := time.Parse("2006-01-02", value)
	if err != nil || date.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("%s must be a date in YYYY-MM-DD format", field)
	}
	return date, nil
}

type ReportWindow struct {
	Start time.Time // UTC calendar date
	End   time.Time // UTC calendar date, inclusive
}

func ActiveWindow(config Schedule, now time.Time) (ReportWindow, error) {
	start, err := ParseCalendarDate("start_date", config.StartDate)
	if err != nil {
		return ReportWindow{}, err
	}
	end, err := ParseCalendarDate("end_date", config.EndDate)
	if err != nil {
		return ReportWindow{}, err
	}
	if config.WindowDays < 1 {
		return ReportWindow{}, fmt.Errorf("window_days must be at least 1")
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if today.Before(start) || today.After(end) {
		return ReportWindow{}, nil
	}
	days := int(today.Sub(start).Hours() / 24)
	windowStart := start.AddDate(0, 0, days/config.WindowDays*config.WindowDays)
	windowEnd := windowStart.AddDate(0, 0, config.WindowDays-1)
	if windowEnd.After(end) {
		windowEnd = end
	}
	return ReportWindow{windowStart, windowEnd}, nil
}
