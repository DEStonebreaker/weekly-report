package app

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	tasksapi "google.golang.org/api/tasks/v1"

	"weekly-report/internal/google"
	"weekly-report/internal/report"
)

var (
	ErrInvalidPreparedReport = errors.New("report was not prepared by this service; prepare a new report")
	ErrPreparedReportUsed    = errors.New("report already attempted; prepare a new report before sending again")
)

// ReportPreview is safe to pass to a UI. Dates are inclusive calendar dates.
type ReportPreview struct {
	Sender     string           `json:"sender"`
	Subject    string           `json:"subject"`
	TaskListID string           `json:"taskListId"`
	StartDate  string           `json:"startDate"`
	EndDate    string           `json:"endDate"`
	Messages   []MessagePreview `json:"messages"`
}

type MessagePreview struct {
	Recipient Recipient `json:"recipient"`
	HTML      string    `json:"html"`
}

// PreparedReport is an opaque, in-memory snapshot, not a frontend input DTO.
// Callers retain its pointer and expose only Preview() to the UI. Do not copy it.
// Editing report inputs requires a new preparation.
type PreparedReport struct {
	owner     *Service
	preview   ReportPreview
	messages  []preparedEmail
	attempted *atomic.Bool
}

// Preview returns a detached copy; changing it cannot alter the email to send.
func (p *PreparedReport) Preview() ReportPreview {
	preview := p.preview
	preview.Messages = append([]MessagePreview(nil), p.preview.Messages...)
	return preview
}

// prepareReport only reads Tasks and builds messages; it has no Gmail or state access.
func prepareReport(ctx context.Context, config Config, window report.ReportWindow, lists []google.TaskList, tasksService *tasksapi.Service) (*PreparedReport, error) {
	listID, err := selectTaskList(lists, config.TaskListID)
	if err != nil {
		return nil, err
	}
	dueMin := window.Start
	// Google Tasks filters calendar dates with an exclusive upper bound.
	// Using the final day's last second can exclude that entire day.
	dueMax := window.End.AddDate(0, 0, 1)
	tasks, err := google.GetTasks(ctx, tasksService, listID, google.GetTasksOptions{DueMin: &dueMin, DueMax: &dueMax})
	if err != nil {
		return nil, fmt.Errorf("get tasks: %w", google.WithAuthHint(err))
	}
	tasks = report.SortByDueDate(tasks)
	prepared := &PreparedReport{attempted: &atomic.Bool{}, preview: ReportPreview{
		Sender: config.Sender, Subject: config.Subject, TaskListID: listID,
		StartDate: window.Start.Format("2006-01-02"), EndDate: window.End.Format("2006-01-02"),
		Messages: make([]MessagePreview, 0, len(config.Recipients)),
	}}
	for _, recipient := range config.Recipients {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, err := report.RenderReport(recipient.Name, tasks, window)
		if err != nil {
			return nil, fmt.Errorf("render report for %s: %w", recipient.Address, err)
		}
		raw, err := google.BuildRawMessage(google.OutgoingEmail{From: config.Sender, To: recipient.Address, Subject: config.Subject, HTMLBody: body}, time.Now())
		if err != nil {
			return nil, fmt.Errorf("prepare report for %s: %w", recipient.Address, err)
		}
		prepared.messages = append(prepared.messages, preparedEmail{recipient: recipient, key: deliveryKey(window, listID, recipient.Address), raw: raw})
		prepared.preview.Messages = append(prepared.preview.Messages, MessagePreview{Recipient: recipient, HTML: body})
	}
	return prepared, nil
}
