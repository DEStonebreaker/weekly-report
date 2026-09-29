package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"weekly-report/internal/google"
	"weekly-report/internal/report"
)

func TestReportFlowPartialFailureAndRepeat(t *testing.T) {
	config := Config{
		Sender: "sender@example.com", Subject: "Report", TaskListID: "list-id",
		StartDate: "2026-09-19", EndDate: "2026-10-05", WindowDays: 7,
		Recipients: []Recipient{{Name: "Alice", Address: "alice@example.com"}, {Name: "Bob", Address: "bob@example.com"}},
	}
	window, err := activeWindow(config, time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var taskCalls, sendCalls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/tasks/v1/lists/list-id/tasks":
			taskCalls.Add(1)
			query := req.URL.Query()
			if got := query.Get("dueMin"); got != "2026-09-19T00:00:00Z" {
				t.Errorf("dueMin=%s", got)
			}
			if got := query.Get("dueMax"); got != "2026-09-26T00:00:00Z" {
				t.Errorf("dueMax=%s", got)
			}
			return jsonResponse(200, `{"items":[{"id":"one","title":"Task","due":"2026-09-20T00:00:00Z"}]}`), nil
		case "/gmail/v1/users/me/messages/send":
			sendCalls.Add(1)
			var message struct {
				Raw string `json:"raw"`
			}
			if err := json.NewDecoder(req.Body).Decode(&message); err != nil {
				t.Errorf("decode: %v", err)
			}
			decoded, err := base64.RawURLEncoding.DecodeString(message.Raw)
			if err != nil {
				t.Errorf("decode raw: %v", err)
			}
			raw := string(decoded)
			if !strings.Contains(raw, "Sep 19, 2026") || !strings.Contains(raw, "Sep 25, 2026") {
				t.Errorf("wrong rendered window: %s", raw)
			}
			if strings.Contains(raw, "bob@example.com") {
				return jsonResponse(400, `{"error":{"code":400,"message":"test failure"}}`), nil
			}
			return jsonResponse(200, `{"id":"sent-id"}`), nil
		default:
			t.Errorf("unexpected API path %s", req.URL.Path)
			return jsonResponse(404, `{}`), nil
		}
	})}
	tasksService, err := google.CreateTasksService(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	state, err := openDeliveryState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	lists := []google.TaskList{{ID: "list-id", Title: "Work"}}
	var output bytes.Buffer
	results, err := deliverReport(context.Background(), config, window, state, lists, tasksService, client)
	printSendResults(&output, deliveryResults(results))
	if err == nil {
		t.Fatal("partial failure did not return error")
	}
	if !strings.Contains(output.String(), "1 sent, 1 failed, 0 uncertain, 0 skipped") {
		t.Errorf("output: %s", output.String())
	}
	if taskCalls.Load() != 1 || sendCalls.Load() != 2 {
		t.Errorf("calls: tasks=%d send=%d", taskCalls.Load(), sendCalls.Load())
	}
	output.Reset()
	results, err = deliverReport(context.Background(), config, window, state, lists, tasksService, client)
	printSendResults(&output, deliveryResults(results))
	if err == nil {
		t.Fatal("prior failure was not reported")
	}
	if taskCalls.Load() != 1 || sendCalls.Load() != 2 {
		t.Errorf("repeat made API calls: tasks=%d send=%d", taskCalls.Load(), sendCalls.Load())
	}
	if !strings.Contains(output.String(), "previous failed attempt") {
		t.Errorf("repeat output: %s", output.String())
	}
}

func TestReportPreparationFailureDoesNotSend(t *testing.T) {
	config := Config{Sender: "sender@example.com", Subject: "Report", TaskListID: "list-id", Recipients: []Recipient{{Name: "Alice", Address: "alice@example.com"}, {Name: "Bad", Address: "bad address"}}}
	window := report.ReportWindow{Start: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)}
	var sends atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/messages/send") {
			sends.Add(1)
			return jsonResponse(200, `{"id":"sent"}`), nil
		}
		return jsonResponse(200, `{"items":[]}`), nil
	})}
	tasksService, err := google.CreateTasksService(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	state, err := openDeliveryState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	_, err = deliverReport(context.Background(), config, window, state, []google.TaskList{{ID: "list-id"}}, tasksService, client)
	if err == nil || !strings.Contains(err.Error(), "prepare report") {
		t.Errorf("error=%v", err)
	}
	if sends.Load() != 0 {
		t.Errorf("sent %d messages", sends.Load())
	}
}

func TestReportTasksUnauthorizedSuggestsInit(t *testing.T) {
	config := Config{Sender: "sender@example.com", Subject: "Report", TaskListID: "list-id", Recipients: []Recipient{{Name: "Alice", Address: "alice@example.com"}}}
	window := report.ReportWindow{Start: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)}
	var taskCalls, sendCalls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/tasks/v1/lists/list-id/tasks" {
			taskCalls.Add(1)
			return jsonResponse(401, `{"error":{"code":401,"message":"Invalid Credentials"}}`), nil
		}
		sendCalls.Add(1)
		return jsonResponse(200, `{"id":"unexpected"}`), nil
	})}
	service, err := google.CreateTasksService(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	state, err := openDeliveryState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	_, err = deliverReport(context.Background(), config, window, state, []google.TaskList{{ID: "list-id"}}, service, client)
	if err == nil || !strings.Contains(err.Error(), "weekly-report --init") {
		t.Errorf("error=%v, want --init guidance", err)
	}
	if taskCalls.Load() != 1 || sendCalls.Load() != 0 {
		t.Errorf("calls: tasks=%d, sends=%d", taskCalls.Load(), sendCalls.Load())
	}
	if _, attempted := state.get(deliveryKey(window, "list-id", "alice@example.com")); attempted {
		t.Error("a delivery attempt was recorded before tasks were fetched")
	}
}
