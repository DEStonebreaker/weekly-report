package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

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

// deliverReport uses state only for scheduled delivery. All messages are prepared
// before any send, and existing attempts are returned in configuration order.
func deliverReport(ctx context.Context, config Config, window report.ReportWindow, state *DeliveryState, lists []google.TaskList, tasksService *tasksapi.Service, client *http.Client) ([]sendResult, error) {
	results, _, _, err := deliverReportDetailed(ctx, config, window, state, lists, tasksService, client)
	return results, err
}

func deliverReportDetailed(ctx context.Context, config Config, window report.ReportWindow, state *DeliveryState, lists []google.TaskList, tasksService *tasksapi.Service, client *http.Client) ([]sendResult, int, bool, error) {
	pending := config
	pending.Recipients = nil
	results, _ := existingResults(config, window, state)
	for i, recipient := range config.Recipients {
		if results[i].status == "" {
			pending.Recipients = append(pending.Recipients, recipient)
		}
	}
	if len(pending.Recipients) == 0 {
		return results, 0, false, resultError(results)
	}
	prepared, err := prepareReportWithFilter(ctx, pending, window, lists, tasksService, true)
	if err != nil {
		return nil, 0, false, err
	}
	if prepared.taskCount == 0 {
		prior := make([]sendResult, 0, len(results))
		for _, result := range results {
			if result.status != "" {
				prior = append(prior, result)
			}
		}
		return prior, 0, true, resultError(prior)
	}
	gmailService, err := google.CreateGmailService(ctx, client)
	if err != nil {
		return nil, prepared.taskCount, false, err
	}
	sent := sendPrepared(ctx, gmailService, prepared.messages, state)
	for i, recipient := range config.Recipients {
		for _, result := range sent {
			if recipient.Address == result.recipient.Address {
				results[i] = result
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return results, prepared.taskCount, false, err
	}
	return results, prepared.taskCount, false, resultError(results)
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
	return sendPreparedWithProgress(ctx, service, prepared, state, nil)
}

func sendPreparedWithProgress(ctx context.Context, service *gmailapi.Service, prepared []preparedEmail, state *DeliveryState, progress func(int, sendResult)) []sendResult {
	results := make([]sendResult, len(prepared))
	slots := make(chan struct{}, 4)
	var group sync.WaitGroup
	var completed atomic.Int32
	finish := func(index int, result sendResult) {
		results[index] = result
		if progress != nil {
			progress(int(completed.Add(1)), result)
		}
	}
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
				finish(index, result)
				return
			}
			if err := ctx.Err(); err != nil {
				result.status = "SKIPPED"
				result.err = err
				finish(index, result)
				return
			}
			if state != nil {
				if err := state.beginAttempt(message.key, false); err != nil {
					result.status = "FAILED"
					result.err = fmt.Errorf("record send attempt: %w", err)
					finish(index, result)
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
			finish(index, result)
		}(index, message)
	}
	group.Wait()
	return results
}
