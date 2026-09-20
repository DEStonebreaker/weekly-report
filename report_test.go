package main

import (
	"strings"
	"testing"
	"time"
)

func TestRenderReport(t *testing.T) {
	firstDue := time.Date(2026, time.September, 14, 0, 0, 0, 0, time.UTC)
	secondDue := time.Date(2026, time.September, 16, 0, 0, 0, 0, time.UTC)

	html, err := RenderReport("Don & Co", []Task{
		{
			Title:       "First <task>",
			DueDate:     &firstDue,
			Notes:       "@23:59\nNotes & details",
			WebViewLink: "https://tasks.google.com/task/first?view=detail&source=report",
		},
		{Title: "No due date", DueDate: nil},
		{Title: "Second task", DueDate: &secondDue},
	}, testWindow())
	if err != nil {
		t.Fatalf("RenderReport() error = %v", err)
	}

	for _, want := range []string{
		"Hey Don &amp; Co!",
		"You have 2 tasks due from Sep 14, 2026 to Sep 20, 2026.",
		"First &lt;task&gt;",
		`href="https://tasks.google.com/task/first?view=detail&amp;source=report"`,
		`style="color:#1F1F1F !important;text-decoration:none !important;"`,
		"Mon, Sep 14",
		"23:59",
		"Notes &amp; details",
		"Second task",
		"Wed, Sep 16",
		"https://github.com/DEStonebreaker/weekly-report",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered report does not contain %q", want)
		}
	}

	if strings.Contains(html, "No due date") {
		t.Error("rendered report contains a task without a due date")
	}

	if strings.Contains(html, `colspan="2"`) {
		t.Error("task description spans into the due-date column")
	}

	if strings.Contains(html, "@23:59") {
		t.Error("rendered report contains the time marker in the description")
	}

	if strings.Index(html, "First &lt;task&gt;") > strings.Index(html, "Second task") {
		t.Error("RenderReport() did not preserve task order")
	}
}

func testWindow() ReportWindow {
	return ReportWindow{Start: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)}
}

func TestToEmailTaskSeparatesTimeFromDescription(t *testing.T) {
	tests := []struct {
		name            string
		notes           string
		wantTime        string
		wantDescription string
	}{
		{
			name:            "time and description",
			notes:           "@09:30\nFinish the assignment",
			wantTime:        "09:30",
			wantDescription: "Finish the assignment",
		},
		{
			name:            "time only",
			notes:           "@23:59",
			wantTime:        "23:59",
			wantDescription: "",
		},
		{
			name:            "time and same-line description",
			notes:           "@23:59 @P3",
			wantTime:        "23:59",
			wantDescription: "@P3",
		},
		{
			name:            "ordinary description",
			notes:           "Bring notes",
			wantTime:        "",
			wantDescription: "Bring notes",
		},
		{
			name:            "invalid time stays in description",
			notes:           "@tomorrow\nBring notes",
			wantTime:        "",
			wantDescription: "@tomorrow\nBring notes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emailTask := ToEmailTask(Task{Notes: tt.notes})
			if emailTask.Time != tt.wantTime {
				t.Errorf("Time = %q, want %q", emailTask.Time, tt.wantTime)
			}
			if emailTask.Description != tt.wantDescription {
				t.Errorf("Description = %q, want %q", emailTask.Description, tt.wantDescription)
			}
		})
	}
}

func TestRenderReportSingularTask(t *testing.T) {
	due := time.Date(2026, time.September, 14, 0, 0, 0, 0, time.UTC)

	html, err := RenderReport("Donovan", []Task{{Title: "Only task", DueDate: &due}}, testWindow())
	if err != nil {
		t.Fatalf("RenderReport() error = %v", err)
	}

	if !strings.Contains(html, "You have 1 task due from Sep 14, 2026 to Sep 20, 2026.") {
		t.Error("rendered report does not use singular task grammar")
	}
}

func TestRenderReportWithoutDatedTasks(t *testing.T) {
	html, err := RenderReport("Donovan", []Task{{Title: "Undated task"}}, testWindow())
	if err != nil {
		t.Fatalf("RenderReport() error = %v", err)
	}

	if !strings.Contains(html, "You have no tasks due from Sep 14, 2026 to Sep 20, 2026.") {
		t.Error("rendered report does not contain the empty-state message")
	}

	if strings.Contains(html, "class=\"t22\"") {
		t.Error("rendered report contains the task section for an empty report")
	}

	if strings.Contains(html, "Undated task") {
		t.Error("rendered report contains an undated task")
	}
}
