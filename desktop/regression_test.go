package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
		if err != nil || status.Status != "complete" || len(status.Results) != 1 {
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
