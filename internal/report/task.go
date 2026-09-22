package report

import (
	"slices"
	"time"
)

type Task struct {
	ID          string
	Title       string
	DueDate     *time.Time
	WebViewLink string
	Notes       string
}

func SortByDueDate(tasks []Task) []Task {
	sorted := slices.Clone(tasks)

	slices.SortFunc(sorted, func(a, b Task) int {
		switch {
		case a.DueDate == nil && b.DueDate == nil:
			return 0
		case a.DueDate == nil:
			return 1
		case b.DueDate == nil:
			return -1
		default:
			return a.DueDate.Compare(*b.DueDate)
		}
	})

	return sorted
}
