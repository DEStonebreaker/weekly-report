package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	gmailapi "google.golang.org/api/gmail/v1"
	tasksapi "google.golang.org/api/tasks/v1"

	"weekly-report/internal/google"
	"weekly-report/internal/report"
)

type preparedEmail struct {
	recipient Recipient
	key       string
	raw       string
}

type sendResult struct {
	recipient Recipient
	status    string
	messageID string
	err       error
}

// sendReport uses state only for scheduled delivery. Manual sends pass nil and
// neither consult nor change scheduled attempts.
func sendReport(ctx context.Context, config Config, window report.ReportWindow, state *DeliveryState, lists []google.TaskList, tasksService *tasksapi.Service, client *http.Client, output io.Writer) error {
	listID, err := selectTaskList(lists, config.TaskListID)
	if err != nil {
		return err
	}
	pending := make([]Recipient, 0, len(config.Recipients))
	results, _ := existingResults(config, window, state)
	for i, recipient := range config.Recipients {
		if results[i].status == "" {
			pending = append(pending, recipient)
		}
	}
	if len(pending) == 0 {
		printSendResults(output, results)
		return resultError(results)
	}
	dueMin := window.Start
	dueMax := window.End.AddDate(0, 0, 1).Add(-time.Second)
	tasks, err := google.GetTasks(ctx, tasksService, listID, google.GetTasksOptions{DueMin: &dueMin, DueMax: &dueMax})
	if err != nil {
		return fmt.Errorf("get tasks: %w", google.WithAuthHint(err))
	}
	tasks = report.SortByDueDate(tasks)
	prepared := make([]preparedEmail, 0, len(pending))
	for _, recipient := range pending {
		body, err := report.RenderReport(recipient.Name, tasks, window)
		if err != nil {
			return fmt.Errorf("render report for %s: %w", recipient.Address, err)
		}
		raw, err := google.BuildRawMessage(google.OutgoingEmail{From: config.Sender, To: recipient.Address, Subject: config.Subject, HTMLBody: body}, time.Now())
		if err != nil {
			return fmt.Errorf("prepare report for %s: %w", recipient.Address, err)
		}
		prepared = append(prepared, preparedEmail{recipient, deliveryKey(window, listID, recipient.Address), raw})
	}
	gmailService, err := google.CreateGmailService(ctx, client)
	if err != nil {
		return err
	}
	sent := sendPrepared(ctx, gmailService, prepared, state)
	for i, recipient := range config.Recipients {
		for _, result := range sent {
			if recipient.Address == result.recipient.Address {
				results[i] = result
				break
			}
		}
	}
	printSendResults(output, results)
	if err := ctx.Err(); err != nil {
		return err
	}
	return resultError(results)
}

func existingResults(config Config, window report.ReportWindow, state *DeliveryState) ([]sendResult, bool) {
	results := make([]sendResult, len(config.Recipients))
	complete := true
	for i, recipient := range config.Recipients {
		results[i].recipient = recipient
		if state == nil {
			complete = false
			continue
		}
		record, ok := state.get(deliveryKey(window, config.TaskListID, recipient.Address))
		if !ok {
			complete = false
			continue
		}
		if record.Status == "sent" {
			results[i].status = "SKIPPED"
			results[i].messageID = record.MessageID
		} else {
			results[i].status = "FAILED"
			if record.Status == "uncertain" {
				results[i].status = "UNCERTAIN"
			}
			results[i].err = fmt.Errorf("previous %s attempt: %s", record.Status, record.Error)
		}
	}
	return results, complete
}

func resultError(results []sendResult) error {
	for _, result := range results {
		if result.status == "FAILED" || result.status == "UNCERTAIN" {
			return fmt.Errorf("one or more report emails failed")
		}
	}
	return nil
}

func sendPrepared(ctx context.Context, service *gmailapi.Service, prepared []preparedEmail, state *DeliveryState) []sendResult {
	results := make([]sendResult, len(prepared))
	slots := make(chan struct{}, 4)
	var group sync.WaitGroup
	for index, message := range prepared {
		group.Add(1)
		go func(index int, message preparedEmail) {
			defer group.Done()
			result := sendResult{recipient: message.recipient}
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				result.status = "SKIPPED"
				result.err = ctx.Err()
				results[index] = result
				return
			}
			if err := ctx.Err(); err != nil {
				result.status = "SKIPPED"
				result.err = err
				results[index] = result
				return
			}
			if state != nil {
				if err := state.beginAttempt(message.key, false); err != nil {
					result.status = "FAILED"
					result.err = fmt.Errorf("record send attempt: %w", err)
					results[index] = result
					return
				}
			}

			id, err := google.SendRawEmail(ctx, service, message.raw)
			record := DeliveryRecord{Status: "sent", MessageID: id}
			result.status = "SENT"
			if err != nil {
				err = google.WithAuthHint(err)
				result.status = "FAILED"
				if google.DeliveryUncertain(err) {
					result.status = "UNCERTAIN"
				}
				record = DeliveryRecord{Status: strings.ToLower(result.status), Error: err.Error()}
			}
			if state != nil {
				if saveErr := state.set(message.key, record); saveErr != nil {
					err = fmt.Errorf("record send result: %w", saveErr)
					result.status = "UNCERTAIN"
				}
			}

			if err != nil {
				result.err = err
			} else {
				result.messageID = id
			}
			results[index] = result
		}(index, message)
	}
	group.Wait()
	return results
}

func printSendResults(output io.Writer, results []sendResult) {
	sent, failed, uncertain, skipped := 0, 0, 0, 0
	for _, result := range results {
		switch result.status {
		case "SENT":
			sent++
			fmt.Fprintf(output, "SENT\t%s\tmessage ID %s\n", result.recipient.Address, result.messageID)
		case "FAILED":
			failed++
			fmt.Fprintf(output, "FAILED\t%s\t%v\n", result.recipient.Address, result.err)
		case "UNCERTAIN":
			uncertain++
			fmt.Fprintf(output, "UNCERTAIN\t%s\t%v\n", result.recipient.Address, result.err)
		default:
			skipped++
			if result.err != nil {
				fmt.Fprintf(output, "SKIPPED\t%s\t%v\n", result.recipient.Address, result.err)
			} else {
				fmt.Fprintf(output, "SKIPPED\t%s\tpreviously attempted\n", result.recipient.Address)
			}
		}
	}
	fmt.Fprintf(output, "%d sent, %d failed, %d uncertain, %d skipped\n", sent, failed, uncertain, skipped)
}
