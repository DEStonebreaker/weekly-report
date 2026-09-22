package report

import (
	"testing"
	"time"
)

func TestSortByDueDateDoesNotModifyInput(t *testing.T) {
	firstDue := time.Date(2026, time.September, 18, 0, 0, 0, 0, time.UTC)
	secondDue := time.Date(2026, time.September, 19, 0, 0, 0, 0, time.UTC)
	input := []Task{
		{ID: "second", DueDate: &secondDue},
		{ID: "undated"},
		{ID: "first", DueDate: &firstDue},
	}

	sorted := SortByDueDate(input)
	if got := []string{sorted[0].ID, sorted[1].ID, sorted[2].ID}; got[0] != "first" || got[1] != "second" || got[2] != "undated" {
		t.Errorf("SortByDueDate() order = %v, want [first second undated]", got)
	}
	if input[0].ID != "second" || input[1].ID != "undated" || input[2].ID != "first" {
		t.Errorf("SortByDueDate() modified input: %#v", input)
	}
}
