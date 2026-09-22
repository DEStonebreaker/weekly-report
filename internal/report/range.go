package report

import (
	"fmt"
	"time"
)

// RangeOptions selects a manual report range. A zero value means seven days
// starting today. Explicit and calendar-week ranges may include past dates.
type RangeOptions struct {
	Days     *int   `help:"Number of calendar days starting today (default: 7)."`
	ThisWeek bool   `help:"Report this Monday through Sunday."`
	NextWeek bool   `help:"Report next Monday through Sunday."`
	From     string `help:"First included date (YYYY-MM-DD); requires --to."`
	To       string `help:"Last included date (YYYY-MM-DD); requires --from."`
}

func (options RangeOptions) Validate() error {
	modes := 0
	if options.Days != nil {
		modes++
		if *options.Days < 1 {
			return fmt.Errorf("--days must be at least 1")
		}
	}
	if options.ThisWeek {
		modes++
	}
	if options.NextWeek {
		modes++
	}
	if options.From != "" || options.To != "" {
		modes++
		if options.From == "" || options.To == "" {
			return fmt.Errorf("--from and --to must be used together")
		}
		start, err := ParseCalendarDate("--from", options.From)
		if err != nil {
			return err
		}
		end, err := ParseCalendarDate("--to", options.To)
		if err != nil {
			return err
		}
		if end.Before(start) {
			return fmt.Errorf("--to must be on or after --from")
		}
	}
	if modes > 1 {
		return fmt.Errorf("choose only one range: --days, --this-week, --next-week, or --from with --to")
	}
	return nil
}

// Resolve uses the machine's local calendar date, represented as a UTC date
// consistently with Google Tasks and scheduled report windows.
func (options RangeOptions) Resolve(now time.Time) (ReportWindow, error) {
	if err := options.Validate(); err != nil {
		return ReportWindow{}, err
	}
	if options.From != "" {
		start, _ := ParseCalendarDate("--from", options.From)
		end, _ := ParseCalendarDate("--to", options.To)
		return ReportWindow{Start: start, End: end}, nil
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	days := 7
	if options.Days != nil {
		days = *options.Days
	}
	if options.ThisWeek || options.NextWeek {
		daysSinceMonday := (int(start.Weekday()) + 6) % 7
		start = start.AddDate(0, 0, -daysSinceMonday)
		if options.NextWeek {
			start = start.AddDate(0, 0, 7)
		}
	}
	return ReportWindow{Start: start, End: start.AddDate(0, 0, days-1)}, nil
}
