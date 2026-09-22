package google

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	tasksapi "google.golang.org/api/tasks/v1"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestGetTasksUsesWindowAndFollowsPagination(t *testing.T) {
	dueMin := time.Date(2026, time.September, 18, 0, 0, 0, 0, time.UTC)
	dueMax := time.Date(2026, time.September, 24, 0, 0, 0, 0, time.UTC)
	var requests int

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if got, want := request.URL.Path, "/tasks/v1/lists/list-id/tasks"; got != want {
			t.Errorf("request path = %q, want %q", got, want)
		}
		query := request.URL.Query()
		if got, want := query.Get("dueMin"), dueMin.Format(time.RFC3339); got != want {
			t.Errorf("dueMin = %q, want %q", got, want)
		}
		if got, want := query.Get("dueMax"), dueMax.Format(time.RFC3339); got != want {
			t.Errorf("dueMax = %q, want %q", got, want)
		}
		if got := query.Get("showCompleted"); got != "false" {
			t.Errorf("showCompleted = %q, want %q", got, "false")
		}

		body := `{"items":[{"id":"second","title":"Second"}]}`
		if requests == 1 {
			if got := query.Get("pageToken"); got != "" {
				t.Errorf("first page token = %q, want empty", got)
			}
			body = `{"items":[{"id":"first","title":"First"}],"nextPageToken":"next"}`
		} else if got := query.Get("pageToken"); got != "next" {
			t.Errorf("second page token = %q, want %q", got, "next")
		}

		return jsonResponse(http.StatusOK, body), nil
	})}
	service := newTestTasksService(t, client)

	tasks, err := GetTasks(context.Background(), service, "list-id", GetTasksOptions{
		DueMin: &dueMin,
		DueMax: &dueMax,
	})
	if err != nil {
		t.Fatalf("GetTasks() error = %v", err)
	}
	if requests != 2 {
		t.Errorf("GetTasks() made %d requests, want 2", requests)
	}
	if len(tasks) != 2 || tasks[0].ID != "first" || tasks[1].ID != "second" {
		t.Errorf("GetTasks() tasks = %#v, want first and second tasks", tasks)
	}
}

func TestGetTaskListsReturnsAPIError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, `{"error":"failure"}`), nil
	})}
	service := newTestTasksService(t, client)

	_, err := GetTaskLists(context.Background(), service)
	if err == nil {
		t.Fatal("GetTaskLists() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("GetTaskLists() error = %q, want status code", err)
	}
}

func TestGetTasksRejectsInvalidDueDate(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"items":[{"id":"bad-date","due":"not-a-date"}]}`), nil
	})}
	service := newTestTasksService(t, client)

	_, err := GetTasks(context.Background(), service, "list-id", GetTasksOptions{})
	if err == nil {
		t.Fatal("GetTasks() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "parse due date") {
		t.Errorf("GetTasks() error = %q, want due-date context", err)
	}
}

func jsonResponse(status int, body string) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newTestTasksService(t *testing.T, client *http.Client) *tasksapi.Service {
	t.Helper()

	service, err := CreateTasksService(context.Background(), client)
	if err != nil {
		t.Fatalf("create test Tasks service: %v", err)
	}
	return service
}
