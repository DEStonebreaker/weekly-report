package main

import (
	"strings"
	"testing"
	"time"
)

func TestReportWindow(t *testing.T) {
	now := time.Date(2026, time.September, 18, 16, 45, 0, 0, time.FixedZone("EDT", -4*60*60))

	dueMin, dueMax, err := reportWindow(now, 7)
	if err != nil {
		t.Fatalf("reportWindow() error = %v", err)
	}

	wantMin := time.Date(2026, time.September, 18, 0, 0, 0, 0, time.UTC)
	wantMax := time.Date(2026, time.September, 24, 23, 59, 59, 0, time.UTC)
	if !dueMin.Equal(wantMin) {
		t.Errorf("reportWindow() minimum = %v, want %v", dueMin, wantMin)
	}
	if !dueMax.Equal(wantMax) {
		t.Errorf("reportWindow() maximum = %v, want %v", dueMax, wantMax)
	}
}

func TestReportWindowRejectsInvalidDays(t *testing.T) {
	_, _, err := reportWindow(time.Now(), 0)
	if err == nil {
		t.Fatal("reportWindow() error = nil, want an error")
	}
}

func TestSelectTaskList(t *testing.T) {
	taskLists := []TaskList{
		{ID: "first", Title: "First"},
		{ID: "configured", Title: "Configured"},
	}

	tests := []struct {
		name         string
		taskLists    []TaskList
		configuredID string
		want         string
		wantErrText  string
	}{
		{
			name:      "defaults to first list",
			taskLists: taskLists,
			want:      "first",
		},
		{
			name:         "uses configured list",
			taskLists:    taskLists,
			configuredID: "configured",
			want:         "configured",
		},
		{
			name:        "rejects empty account",
			wantErrText: "no task lists",
		},
		{
			name:         "rejects unknown configured list",
			taskLists:    taskLists,
			configuredID: "missing",
			wantErrText:  "was not found",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := selectTaskList(test.taskLists, test.configuredID)
			if test.wantErrText != "" {
				if err == nil {
					t.Fatal("selectTaskList() error = nil, want an error")
				}
				if !strings.Contains(err.Error(), test.wantErrText) {
					t.Errorf("selectTaskList() error = %q, want it to contain %q", err, test.wantErrText)
				}
				return
			}

			if err != nil {
				t.Fatalf("selectTaskList() error = %v", err)
			}
			if got != test.want {
				t.Errorf("selectTaskList() = %q, want %q", got, test.want)
			}
		})
	}
}
