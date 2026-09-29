package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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
	"weekly-report/internal/report"
)

func setupManualReport(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTestCredentials(t, dir)
	if err := saveTokenFile(filepath.Join(dir, "token.json"), &oauth2.Token{AccessToken: "access", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	config := `sender="sender@example.com"
subject="Report"
task_list_id="list-id"
start_date="2026-09-19"
end_date="2026-09-25"
window_days=7
[[recipients]]
name="Alice"
address="alice@example.com"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// This fake checks both the API bounds and the dates rendered into the email.
func reportClient(t *testing.T, start, end string, sends *int) context.Context {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/tasks/v1/users/@me/lists":
			return jsonResponse(200, `{"items":[{"id":"list-id","title":"Work"}]}`), nil
		case "/tasks/v1/lists/list-id/tasks":
			if got := req.URL.Query().Get("dueMin"); got != start+"T00:00:00Z" {
				t.Errorf("dueMin=%s", got)
			}
			lastDay, _ := time.Parse("2006-01-02", end)
			if got := req.URL.Query().Get("dueMax"); got != lastDay.AddDate(0, 0, 1).Format(time.RFC3339) {
				t.Errorf("dueMax=%s", got)
			}
			// Model the date-only, exclusive upper bound observed from Google:
			// Sunday 23:59:59 excludes Sunday; Monday 00:00 includes it.
			firstDay, _ := time.Parse("2006-01-02", start)
			minDate := strings.SplitN(req.URL.Query().Get("dueMin"), "T", 2)[0]
			maxDate := strings.SplitN(req.URL.Query().Get("dueMax"), "T", 2)[0]
			var items []map[string]string
			for _, task := range []struct{ title, date string }{
				{"Before range task", firstDay.AddDate(0, 0, -1).Format("2006-01-02")},
				{"First boundary task", start},
				{"Last boundary task", end},
				{"After range task", lastDay.AddDate(0, 0, 1).Format("2006-01-02")},
			} {
				if task.date >= minDate && task.date < maxDate {
					items = append(items, map[string]string{"id": task.title, "title": task.title, "due": task.date + "T00:00:00Z"})
				}
			}
			body, err := json.Marshal(map[string]any{"items": items})
			if err != nil {
				t.Fatal(err)
			}
			return jsonResponse(200, string(body)), nil
		case "/gmail/v1/users/me/messages/send":
			(*sends)++
			var body struct{ Raw string }
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			decoded, err := base64.RawURLEncoding.DecodeString(body.Raw)
			if err != nil {
				t.Error(err)
			}
			message, err := mail.ReadMessage(bytes.NewReader(decoded))
			if err != nil {
				t.Error(err)
				return jsonResponse(400, `{}`), nil
			}
			html, err := io.ReadAll(quotedprintable.NewReader(message.Body))
			if err != nil {
				t.Error(err)
			}
			for _, date := range []string{start, end} {
				day, _ := time.Parse("2006-01-02", date)
				if !strings.Contains(string(html), day.Format("Jan 2, 2006")) {
					t.Errorf("email missing range date %s", date)
				}
			}
			for _, title := range []string{"First boundary task", "Last boundary task"} {
				if !strings.Contains(string(html), title) {
					t.Errorf("email missing %s", title)
				}
			}
			for _, title := range []string{"Before range task", "After range task"} {
				if strings.Contains(string(html), title) {
					t.Errorf("email includes out-of-range task %s", title)
				}
			}
			return jsonResponse(200, `{"id":"sent"}`), nil
		default:
			t.Errorf("unexpected request %s", req.URL)
			return jsonResponse(404, `{}`), nil
		}
	})}
	return context.WithValue(context.Background(), oauth2.HTTPClient, client)
}

func TestManualRangesOutsideSchedule(t *testing.T) {
	fourteen := 14
	for _, tc := range []struct {
		name, today string
		options     report.RangeOptions
		start, end  string
	}{
		{"before schedule", "2026-09-10", report.RangeOptions{}, "2026-09-10", "2026-09-16"},
		{"after schedule", "2026-10-01", report.RangeOptions{}, "2026-10-01", "2026-10-07"},
		{"default crosses window", "2026-09-22", report.RangeOptions{}, "2026-09-22", "2026-09-28"},
		{"days", "2026-09-22", report.RangeOptions{Days: &fourteen}, "2026-09-22", "2026-10-05"},
		{"this week", "2026-09-22", report.RangeOptions{ThisWeek: true}, "2026-09-21", "2026-09-27"},
		{"next week", "2026-09-22", report.RangeOptions{NextWeek: true}, "2026-09-28", "2026-10-04"},
		{"explicit past", "2026-09-22", report.RangeOptions{From: "2026-08-01", To: "2026-08-03"}, "2026-08-01", "2026-08-03"},
		{"single day", "2026-09-22", report.RangeOptions{From: "2026-09-27", To: "2026-09-27"}, "2026-09-27", "2026-09-27"},
		{"year boundary", "2026-12-31", report.RangeOptions{ThisWeek: true}, "2026-12-28", "2027-01-03"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupManualReport(t)
			sends := 0
			ctx := reportClient(t, tc.start, tc.end, &sends)
			now, _ := time.Parse("2006-01-02", tc.today)
			service := NewService(dir)
			config, err := service.LoadSettings()
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := service.PrepareReport(ctx, config, tc.options, now)
			if err != nil {
				t.Fatal(err)
			}
			preview := prepared.Preview()
			if preview.StartDate != tc.start || preview.EndDate != tc.end || len(preview.Messages) != 1 {
				t.Fatalf("unexpected preview: %+v", preview)
			}
			html := preview.Messages[0].HTML
			if !strings.Contains(html, "First boundary task") || !strings.Contains(html, "Last boundary task") || strings.Contains(html, "Before range task") || strings.Contains(html, "After range task") {
				t.Fatal("preview does not contain exactly the in-range tasks")
			}
			if sends != 0 {
				t.Fatal("preparing a preview sent email")
			}
			for i := 0; i < 2; i++ {
				if err := runModeInDir(ctx, Options{ManualSend: true, Range: tc.options}, now, &bytes.Buffer{}, dir, nil); err != nil {
					t.Fatal(err)
				}
			}
			if sends != 2 {
				t.Errorf("sends=%d, want 2", sends)
			}
			for _, name := range []string{"state.json", "state.lock"} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Errorf("manual send created %s: %v", name, err)
				}
			}
		})
	}
}

func TestManualSendPreservesScheduledState(t *testing.T) {
	for _, status := range []string{"sent", "failed", "uncertain"} {
		t.Run(status, func(t *testing.T) {
			dir := setupManualReport(t)
			state, err := openDeliveryState(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer state.Close() // A held schedule lock must not block a manual send.
			if err := state.set(deliveryKey(report.ReportWindow{Start: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)}, "list-id", "alice@example.com"), DeliveryRecord{Status: status}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "state.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			sends := 0
			ctx := reportClient(t, "2026-09-22", "2026-09-28", &sends)
			now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
			if err := runModeInDir(ctx, Options{ManualSend: true}, now, &bytes.Buffer{}, dir, nil); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("manual send changed scheduled state")
			}
			if sends != 1 {
				t.Errorf("sends=%d", sends)
			}
		})
	}
}

func TestScheduledSendAfterManualUsesFixedWindow(t *testing.T) {
	dir := setupManualReport(t)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	sends := 0
	manual := reportClient(t, "2026-09-22", "2026-09-28", &sends)
	if err := runModeInDir(manual, Options{ManualSend: true}, now, &bytes.Buffer{}, dir, nil); err != nil {
		t.Fatal(err)
	}
	scheduled := reportClient(t, "2026-09-19", "2026-09-25", &sends)
	for i := 0; i < 2; i++ {
		if err := runModeInDir(scheduled, Options{}, now, &bytes.Buffer{}, dir, nil); err != nil {
			t.Fatal(err)
		}
	}
	if sends != 2 {
		t.Errorf("sends=%d, want one manual and one scheduled", sends)
	}
}
