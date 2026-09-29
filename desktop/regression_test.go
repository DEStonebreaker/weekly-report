package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"weekly-report/internal/app"
)

func TestSetupRecipientsSerializeAsArray(t *testing.T) {
	for _, draft := range []bool{false, true} {
		t.Run(map[bool]string{false: "first launch", true: "incomplete settings"}[draft], func(t *testing.T) {
			dir := t.TempDir()
			if draft {
				if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("subject = 'Draft'\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			a := &App{service: app.NewService(dir)}
			setup, err := a.GetSetupData()
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(setup)
			if err != nil || !strings.Contains(string(data), `"recipients":[]`) {
				t.Fatalf("setup recipients must be an array: %s, error=%v", data, err)
			}
		})
	}
}

func desktopServiceFixture(t *testing.T) (*app.Service, app.Config) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"credentials.json": `{"installed":{"client_id":"test","client_secret":"test","auth_uri":"https://example.invalid/auth","token_uri":"https://example.invalid/token","redirect_uris":["http://localhost"]}}`,
		"token.json":       `{"access_token":"test","refresh_token":"test","expiry":"2099-01-01T00:00:00Z"}`,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := app.NewService(dir)
	config := app.Config{Sender: "sender@example.com", Subject: "Report", TaskListID: "list", StartDate: "2026-09-21", EndDate: "2026-09-27", WindowDays: 7, ScheduleEnabled: true, ScheduleTime: "09:00", Recipients: []app.Recipient{{Name: "Alice", Address: "alice@example.com"}}}
	if err := s.SaveSettings(config); err != nil {
		t.Fatal(err)
	}
	return s, config
}

func TestReconnectInvalidatesPreviewAndBlocksReportOperations(t *testing.T) {
	service, config := desktopServiceFixture(t)
	a := &App{ctx: context.Background(), service: service, prepared: &app.PreparedReport{}, preparedID: "old", prepareSeq: 7}
	declined := errors.New("authorization declined")
	err := a.connectGoogle(func(context.Context, *oauth2.Config) (*oauth2.Token, error) {
		if a.prepared != nil || a.preparedID != "" || a.prepareSeq != 8 {
			t.Fatal("reconnect did not invalidate existing and in-flight preparations")
		}
		if _, err := a.PrepareReport(config, "thisWeek", "", "", 0); err == nil {
			t.Error("preparation allowed during reconnect")
		}
		if outcome := a.SendPreparedReport("old"); outcome.Error == "" {
			t.Error("send allowed during reconnect")
		}
		if err := a.connectGoogle(nil); err == nil {
			t.Error("overlapping reconnect allowed")
		}
		return nil, declined
	})
	if !errors.Is(err, declined) || a.connecting {
		t.Fatalf("reconnect error=%v, connecting=%v", err, a.connecting)
	}
	if outcome := a.SendPreparedReport("old"); !strings.Contains(outcome.Error, "expired") {
		t.Fatalf("old preview survived failed reconnect: %+v", outcome)
	}
	a.sending = true
	if err := a.connectGoogle(nil); err == nil {
		t.Fatal("reconnect allowed during a send")
	}
	a.sending = false
	a.prepared, a.preparedID = &app.PreparedReport{}, "another-preview"
	err = a.connectGoogle(func(context.Context, *oauth2.Config) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "renewed", RefreshToken: "renewed", Expiry: time.Now().Add(time.Hour)}, nil
	})
	if err != nil || a.connecting || a.prepared != nil || a.preparedID != "" {
		t.Fatalf("successful renewal left stale preparation: error=%v", err)
	}
}

type desktopTransport func(*http.Request) (*http.Response, error)

func (f desktopTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestScheduledWakeBeforeTodaysTriggerSendsOnce(t *testing.T) {
	service, config := desktopServiceFixture(t)
	sends := 0
	client := &http.Client{Transport: desktopTransport(func(r *http.Request) (*http.Response, error) {
		body := "{}"
		switch r.URL.Path {
		case "/tasks/v1/users/@me/lists":
			body = `{"items":[{"id":"list","title":"Work"}]}`
		case "/tasks/v1/lists/list/tasks":
			body = `{"items":[{"id":"task","title":"Work","due":"2026-09-22T00:00:00Z"}]}`
		case "/gmail/v1/users/me/messages/send":
			sends++
			body = `{"id":"sent"}`
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	wake := time.Date(2026, 9, 22, 8, 0, 0, 0, time.Local)
	for i := 0; i < 2; i++ {
		status, err := scheduledDelivery(ctx, service, wake)
		want := "complete"
		if i == 1 {
			want = "already-sent"
		}
		if err != nil || status.Status != want || len(status.Results) != 1 {
			t.Fatalf("wake delivery status=%+v, error=%v", status, err)
		}
	}
	if sends != 1 {
		t.Fatalf("Gmail sends=%d, want one", sends)
	}
	status, err := scheduledDelivery(ctx, service, wake.AddDate(0, 0, 7))
	if err != nil || status.Status != "no-window" {
		t.Fatalf("expired window status=%+v, error=%v", status, err)
	}
	config.ScheduleEnabled = false
	if err := service.SaveSettings(config); err != nil {
		t.Fatal(err)
	}
	status, err = scheduledDelivery(ctx, service, wake)
	if err != nil || status.Status != "disabled" || sends != 1 {
		t.Fatalf("disabled delivery status=%+v, sends=%d, error=%v", status, sends, err)
	}
}

func TestScheduledEmptyTasksDoesNotSendOrConsumeAttempt(t *testing.T) {
	service, _ := desktopServiceFixture(t)
	response := `{"items":[]}`
	reads, sends := 0, 0
	client := &http.Client{Transport: desktopTransport(func(r *http.Request) (*http.Response, error) {
		body := "{}"
		switch r.URL.Path {
		case "/tasks/v1/users/@me/lists":
			body = `{"items":[{"id":"list","title":"Work"}]}`
		case "/tasks/v1/lists/list/tasks":
			reads++
			if got := r.URL.Query().Get("dueMin"); got != "2026-09-21T00:00:00Z" {
				t.Errorf("dueMin=%s", got)
			}
			if got := r.URL.Query().Get("dueMax"); got != "2026-09-28T00:00:00Z" {
				t.Errorf("dueMax=%s", got)
			}
			body = response
		case "/gmail/v1/users/me/messages/send":
			sends++
			var payload struct{ Raw string }
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode sent message: %v", err)
			} else if raw, err := base64.RawURLEncoding.DecodeString(payload.Raw); err != nil {
				t.Errorf("decode raw message: %v", err)
			} else if message, err := mail.ReadMessage(bytes.NewReader(raw)); err != nil {
				t.Errorf("parse sent message: %v", err)
			} else if html, err := io.ReadAll(quotedprintable.NewReader(message.Body)); err != nil {
				t.Errorf("decode HTML: %v", err)
			} else if !strings.Contains(string(html), "Start boundary") || !strings.Contains(string(html), "End boundary") {
				t.Errorf("sent HTML omitted a boundary task")
			}
			body = `{"id":"sent-after-tasks"}`
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	now := time.Date(2026, 9, 22, 9, 0, 0, 0, time.Local)
	for _, empty := range []string{`{"items":[]}`, `{"items":[{"id":"undated","title":"Undated"}]}`, `{"items":[{"id":"outside","title":"Outside","due":"2026-09-28T00:00:00Z"}]}`} {
		response = empty
		status, err := scheduledDelivery(ctx, service, now)
		if err != nil || status.Status != "no-tasks" || status.TaskCount == nil || *status.TaskCount != 0 || sends != 0 {
			t.Fatalf("empty scheduled run: status=%+v, sends=%d, error=%v", status, sends, err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(service.CredentialsPath()), "state.json")); !os.IsNotExist(err) {
			t.Fatalf("empty run recorded delivery state: %v", err)
		}
	}
	response = `{"items":[{"id":"start","title":"Start boundary","due":"2026-09-21T00:00:00Z"},{"id":"end","title":"End boundary","due":"2026-09-27T00:00:00Z"}]}`
	status, err := scheduledDelivery(ctx, service, now)
	if err != nil || status.Status != "complete" || status.TaskCount == nil || *status.TaskCount != 2 || sends != 1 || reads != 4 {
		t.Fatalf("retry with tasks: status=%+v, reads=%d, sends=%d, error=%v", status, reads, sends, err)
	}
	status, err = scheduledDelivery(ctx, service, now)
	if err != nil || status.Status != "already-sent" || status.TaskCount != nil || status.Results[0].MessageID != "sent-after-tasks" || reads != 4 || sends != 1 {
		t.Fatalf("deduped run: status=%+v, reads=%d, sends=%d, error=%v", status, reads, sends, err)
	}
}

func TestForceSendUsesSelectedDatesWithoutChangingScheduledAttempts(t *testing.T) {
	service, config := desktopServiceFixture(t)
	config.StartDate = "2026-09-19"
	config.EndDate = "2026-10-05"
	if err := service.SaveSettings(config); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(service.CredentialsPath())
	sends := 0
	client := &http.Client{Transport: desktopTransport(func(r *http.Request) (*http.Response, error) {
		body := "{}"
		switch r.URL.Path {
		case "/tasks/v1/users/@me/lists":
			body = `{"items":[{"id":"list","title":"Work"}]}`
		case "/tasks/v1/lists/list/tasks":
			min, max := r.URL.Query().Get("dueMin"), r.URL.Query().Get("dueMax")
			items := []string{}
			for _, task := range []struct{ due, item string }{
				{"2026-09-22T00:00:00Z", `{"id":"first","title":"First window task","due":"2026-09-22T00:00:00Z"}`},
				{"2026-09-26T00:00:00Z", `{"id":"boundary","title":"Boundary task","due":"2026-09-26T00:00:00Z"}`},
			} {
				if min <= task.due && task.due < max {
					items = append(items, task.item)
				}
			}
			body = `{"items":[` + strings.Join(items, ",") + `]}`
		case "/gmail/v1/users/me/messages/send":
			sends++
			body = `{"id":"sent"}`
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.Local)
	if _, err := service.SendScheduledReport(ctx, now); err != nil || sends != 1 {
		t.Fatalf("initial scheduled send: sends=%d, error=%v", sends, err)
	}
	statePath := filepath.Join(dir, "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{ctx: ctx, service: service}
	options, err := manualRange("thisWeek", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		force, err := a.prepareForceSendAt("thisWeek", "", "", uint64(i), now)
		if err != nil {
			t.Fatal(err)
		}
		preview := force.Preview
		if preview.StartDate != "2026-09-21" || preview.EndDate != "2026-09-27" || len(preview.Messages) != 1 || !strings.Contains(preview.Messages[0].HTML, "Boundary task") {
			t.Fatalf("force preview did not use this week: %+v", preview)
		}
		normal, err := service.PrepareReport(ctx, config, options, now)
		if err != nil {
			t.Fatal(err)
		}
		if preview.Messages[0].HTML != normal.Preview().Messages[0].HTML {
			t.Fatal("force preview body differs from normal preview for the same selected dates")
		}
		results, err := service.SendPreparedReport(ctx, a.prepared)
		if err != nil || len(results) != 1 || results[0].Status != "SENT" {
			t.Fatalf("force send %d: results=%+v, error=%v", i+1, results, err)
		}
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("force send changed scheduled delivery state")
	}
	if _, err := service.SendScheduledReport(ctx, now); err != nil || sends != 3 {
		t.Fatalf("scheduled repeat after force send: sends=%d, error=%v", sends, err)
	}
}
