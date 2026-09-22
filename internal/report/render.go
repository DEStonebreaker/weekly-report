package report

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"strings"
	"time"
	"unicode"
)

//go:embed report-template.html
var reportTemplateHTML string

var reportTemplate = template.Must(
	template.New("weekly-report").Parse(reportTemplateHTML),
)

type Report struct {
	Name      string
	StartDate string
	EndDate   string
	TaskCount int
	Tasks     []EmailTask
}

type EmailTask struct {
	Title       string
	Due         string
	Time        string
	Description string
	WebViewLink string
}

func ToEmailTask(task Task) EmailTask {
	var due string

	if task.DueDate != nil {
		due = task.DueDate.Format("Mon, Jan 2")
	}
	taskTime, description := splitTaskNotes(task.Notes)

	return EmailTask{
		Title:       task.Title,
		Due:         due,
		Time:        taskTime,
		Description: description,
		WebViewLink: task.WebViewLink,
	}
}

func splitTaskNotes(notes string) (string, string) {
	notes = strings.TrimSpace(notes)
	if !strings.HasPrefix(notes, "@") {
		return "", notes
	}

	timeToken := notes
	description := ""
	if separator := strings.IndexFunc(notes, unicode.IsSpace); separator >= 0 {
		timeToken = notes[:separator]
		description = strings.TrimSpace(notes[separator:])
	}

	clock := strings.TrimPrefix(timeToken, "@")
	parsed, err := time.Parse("15:04", clock)
	if err != nil {
		return "", notes
	}

	return parsed.Format("15:04"), description
}

// RenderReport renders the weekly report as HTML. Tasks without a due date are
// omitted, and the order of the remaining tasks is preserved.
func RenderReport(name string, tasks []Task, window ReportWindow) (string, error) {
	emailTasks := make([]EmailTask, 0, len(tasks))
	for _, task := range tasks {
		if task.DueDate == nil {
			continue
		}

		emailTasks = append(emailTasks, ToEmailTask(task))
	}

	report := Report{
		Name:      name,
		StartDate: window.Start.Format("Jan 2, 2006"),
		EndDate:   window.End.Format("Jan 2, 2006"),
		TaskCount: len(emailTasks),
		Tasks:     emailTasks,
	}

	var rendered bytes.Buffer
	if err := reportTemplate.Execute(&rendered, report); err != nil {
		return "", fmt.Errorf("render weekly report: %w", err)
	}

	return rendered.String(), nil
}
