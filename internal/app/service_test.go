package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"weekly-report/internal/report"
)

var serviceToday = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

func serviceFixture(t *testing.T) (*Service, Config) {
	t.Helper()
	s := NewService(setupManualReport(t))
	config, err := s.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	return s, config
}

// All provider requests stay in this transport. A send callback is deliberately
// separate so preview tests fail on any Gmail access, including an unexpected read.
func serviceContext(t *testing.T, reads *atomic.Int32, send func(*http.Request) (*http.Response, error)) context.Context {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/tasks/v1/users/@me/lists":
			return jsonResponse(200, `{"items":[{"id":"list-id","title":"Work"}]}`), nil
		case "/tasks/v1/lists/list-id/tasks":
			reads.Add(1)
			return jsonResponse(200, `{"items":[{"id":"one","title":"Reviewed task","due":"2026-09-22T00:00:00Z"}]}`), nil
		case "/gmail/v1/users/me/messages/send":
			if send != nil {
				return send(req)
			}
		}
		t.Errorf("unexpected request: %s %s", req.Method, req.URL)
		return jsonResponse(400, `{}`), nil
	})}
	return context.WithValue(context.Background(), oauth2.HTTPClient, client)
}

func decodedEmail(t *testing.T, req *http.Request) (*mail.Message, string) {
	t.Helper()
	var body struct{ Raw string }
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Error(err)
		return nil, ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(body.Raw)
	if err != nil {
		t.Error(err)
		return nil, ""
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Error(err)
		return nil, ""
	}
	html, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Error(err)
	}
	return message, string(html)
}

func TestServicePreviewIsDetachedAndSendsExactSnapshot(t *testing.T) {
	s, config := serviceFixture(t)
	var reads, sends atomic.Int32
	previewCtx := serviceContext(t, &reads, nil)
	prepared, err := s.PrepareReport(previewCtx, config, report.RangeOptions{}, serviceToday)
	if err != nil {
		t.Fatal(err)
	}
	original := prepared.Preview()
	if len(original.Messages) != 1 || original.StartDate != "2026-09-22" || original.EndDate != "2026-09-28" || !strings.Contains(original.Messages[0].HTML, "Reviewed task") {
		t.Fatalf("unexpected preview: %+v", original)
	}
	for _, name := range []string{"state.json", "state.lock"} {
		if _, err := os.Stat(filepath.Join(s.appDir, name)); !os.IsNotExist(err) {
			t.Fatalf("preview touched %s: %v", name, err)
		}
	}
	// Neither UI mutations nor later settings changes can rewrite approved content.
	edited := prepared.Preview()
	edited.Messages[0].HTML = "replaced"
	edited.Messages[0].Recipient.Address = "wrong@example.com"
	config.Recipients[0].Address = "new@example.com"
	config.Subject = "New subject"
	if err := s.SaveSettings(config); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared.Preview(), original) {
		t.Fatal("preview mutation changed snapshot")
	}
	ctx := serviceContext(t, &reads, func(req *http.Request) (*http.Response, error) {
		sends.Add(1)
		message, html := decodedEmail(t, req)
		// MIME canonicalizes line endings to CRLF; compare decoded HTML as text.
		if message != nil && (!strings.Contains(message.Header.Get("To"), "alice@example.com") || message.Header.Get("Subject") != original.Subject || strings.ReplaceAll(html, "\r\n", "\n") != original.Messages[0].HTML) {
			t.Error("sent message differs from the reviewed preview")
		}
		return jsonResponse(200, `{"id":"sent-id"}`), nil
	})
	results, err := s.SendPreparedReport(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "SENT" || results[0].MessageID != "sent-id" || results[0].Recipient.Address != "alice@example.com" {
		t.Fatalf("results: %+v", results)
	}
	// Even a shallow copy of the opaque value shares the consumption guard.
	copyOfPrepared := *prepared
	if _, err := s.SendPreparedReport(ctx, &copyOfPrepared); !errors.Is(err, ErrPreparedReportUsed) {
		t.Fatalf("repeat error: %v", err)
	}
	if reads.Load() != 1 || sends.Load() != 1 {
		t.Fatalf("reads=%d sends=%d", reads.Load(), sends.Load())
	}
	for _, name := range []string{"state.json", "state.lock"} {
		if _, err := os.Stat(filepath.Join(s.appDir, name)); !os.IsNotExist(err) {
			t.Fatalf("manual delivery touched %s: %v", name, err)
		}
	}
}

func TestServiceConcurrentSendClaimsSnapshotOnce(t *testing.T) {
	s, config := serviceFixture(t)
	var reads, sends atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	ctx := serviceContext(t, &reads, func(req *http.Request) (*http.Response, error) {
		sends.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return jsonResponse(200, `{"id":"sent"}`), nil
	})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	prepared, err := s.PrepareReport(ctx, config, report.RangeOptions{}, serviceToday)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.SendPreparedReport(ctx, prepared); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("send did not start")
	}
	if _, err := s.SendPreparedReport(ctx, prepared); !errors.Is(err, ErrPreparedReportUsed) {
		t.Errorf("concurrent send: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if sends.Load() != 1 {
		t.Fatalf("sent %d times", sends.Load())
	}
}

func TestServiceReturnsOrderedPartialResults(t *testing.T) {
	s, config := serviceFixture(t)
	config.Recipients = append(config.Recipients, Recipient{Name: "Bob", Address: "bob@example.com"}, Recipient{Name: "Carol", Address: "carol@example.com"})
	var reads atomic.Int32
	ctx := serviceContext(t, &reads, func(req *http.Request) (*http.Response, error) {
		message, _ := decodedEmail(t, req)
		if message == nil {
			return jsonResponse(400, `{}`), nil
		}
		switch {
		case strings.Contains(message.Header.Get("To"), "bob@example.com"):
			return jsonResponse(400, `{"error":{"code":400,"message":"rejected"}}`), nil
		case strings.Contains(message.Header.Get("To"), "carol@example.com"):
			return nil, errors.New("connection lost after request")
		default:
			return jsonResponse(200, `{"id":"sent-id"}`), nil
		}
	})
	prepared, err := s.PrepareReport(ctx, config, report.RangeOptions{}, serviceToday)
	if err != nil {
		t.Fatal(err)
	}
	progress := make(chan DeliveryResult, 3)
	results, err := s.SendPreparedReportWithProgress(ctx, prepared, func(_ int, result DeliveryResult) { progress <- result })
	if err == nil || len(results) != 3 {
		t.Fatalf("results=%+v error=%v", results, err)
	}
	seenProgress := map[string]string{}
	for range 3 {
		result := <-progress
		seenProgress[result.Recipient.Address] = result.Status
	}
	for i, status := range []string{"SENT", "FAILED", "UNCERTAIN"} {
		if results[i].Status != status || results[i].Recipient != config.Recipients[i] {
			t.Errorf("result %d: %+v", i, results[i])
		}
		if i > 0 && results[i].Error == "" {
			t.Errorf("missing error for %s", status)
		}
		if seenProgress[config.Recipients[i].Address] != status {
			t.Errorf("progress for %s = %s, want %s", config.Recipients[i].Address, seenProgress[config.Recipients[i].Address], status)
		}
	}
	data, err := json.Marshal(results)
	if err != nil || !bytes.Contains(data, []byte(`"error":"`)) || !bytes.Contains(data, []byte(`"messageId":"sent-id"`)) {
		t.Fatalf("serialized results: %s, %v", data, err)
	}
	if _, err := s.SendPreparedReport(ctx, prepared); !errors.Is(err, ErrPreparedReportUsed) {
		t.Fatalf("uncertain snapshot could be retried: %v", err)
	}
}

func TestServiceReportsSkippedRecipientsWhenAuthorizationIsMissing(t *testing.T) {
	s, config := serviceFixture(t)
	var reads atomic.Int32
	ctx := serviceContext(t, &reads, nil)
	prepared, err := s.PrepareReport(ctx, config, report.RangeOptions{}, serviceToday)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.appDir, "token.json")); err != nil {
		t.Fatal(err)
	}
	progress := make(chan DeliveryResult, len(config.Recipients))
	results, err := s.SendPreparedReportWithProgress(ctx, prepared, func(_ int, result DeliveryResult) { progress <- result })
	if err == nil || len(results) != len(config.Recipients) {
		t.Fatalf("results=%+v error=%v", results, err)
	}
	for i, result := range results {
		if result.Status != "SKIPPED" || result.Recipient != config.Recipients[i] || !strings.Contains(result.Error, "token") {
			t.Errorf("result %d: %+v", i, result)
		}
		if event := <-progress; event.Status != "SKIPPED" {
			t.Errorf("progress result = %+v", event)
		}
	}
}

func TestServiceRejectsInvalidPreparationBeforeNetwork(t *testing.T) {
	s, config := serviceFixture(t)
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Errorf("unexpected network call: %s", req.URL)
		return jsonResponse(400, `{}`), nil
	})})
	bad := config
	bad.Subject = ""
	if _, err := s.PrepareReport(ctx, bad, report.RangeOptions{}, serviceToday); err == nil {
		t.Error("invalid config accepted")
	}
	if _, err := s.PrepareReport(ctx, config, report.RangeOptions{From: "2026-09-22"}, serviceToday); err == nil {
		t.Error("invalid range accepted")
	}
	for _, prepared := range []*PreparedReport{nil, {}} {
		if _, err := s.SendPreparedReport(ctx, prepared); !errors.Is(err, ErrInvalidPreparedReport) {
			t.Errorf("invalid snapshot: %v", err)
		}
	}
}

func TestServiceCancellationAndSnapshotOwnership(t *testing.T) {
	s, config := serviceFixture(t)
	var reads, sends atomic.Int32
	ctx := serviceContext(t, &reads, func(req *http.Request) (*http.Response, error) {
		sends.Add(1)
		return jsonResponse(200, `{"id":"sent"}`), nil
	})
	prepared, err := s.PrepareReport(ctx, config, report.RangeOptions{}, serviceToday)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(s.appDir).SendPreparedReport(ctx, prepared); !errors.Is(err, ErrInvalidPreparedReport) {
		t.Fatalf("foreign snapshot: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.SendPreparedReport(canceled, prepared); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled send: %v", err)
	}
	if sends.Load() != 0 {
		t.Fatal("canceled send made a request")
	}
	if _, err := s.SendPreparedReport(ctx, prepared); err != nil {
		t.Fatalf("pre-start cancellation consumed snapshot: %v", err)
	}
}

func TestServiceSettingsRoundTripAndInvalidSavePreservesFile(t *testing.T) {
	_, config := serviceFixture(t)
	s := NewService(filepath.Join(t.TempDir(), "new-settings"))
	if _, err := s.LoadSettings(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing settings: %v", err)
	}
	config.Subject = "Report with ünicode and \"quotes\""
	config.Recipients = append(config.Recipients, Recipient{Name: "Bob", Address: "bob@example.com"})
	if err := s.SaveSettings(config); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadSettings()
	if err != nil || !reflect.DeepEqual(got, config) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	path := filepath.Join(s.appDir, "config.toml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	config.Recipients[1].Address = config.Recipients[0].Address
	if err := s.SaveSettings(config); err == nil {
		t.Fatal("invalid config saved")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("invalid save changed config: %v", err)
	}
	files, err := os.ReadDir(s.appDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary files left behind: %v %v", files, err)
	}
}

func TestLoadSettingsDraftAllowsIncompleteOnboardingForm(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("subject = \"Draft\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := NewService(dir).LoadSettingsDraft()
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Draft" || got.TaskListID != "" {
		t.Fatalf("draft settings = %#v", got)
	}
	if _, err := NewService(dir).LoadSettings(); err == nil {
		t.Fatal("incomplete settings unexpectedly passed full validation")
	}
}

func TestServiceCancellationReturnsAttemptedAndQueuedResults(t *testing.T) {
	s, config := serviceFixture(t)
	config.Recipients = nil
	for i := 0; i < 5; i++ {
		config.Recipients = append(config.Recipients, Recipient{Name: fmt.Sprintf("Person %d", i), Address: fmt.Sprintf("person%d@example.com", i)})
	}
	var reads, sends atomic.Int32
	entered := make(chan struct{}, 4)
	ctx := serviceContext(t, &reads, func(req *http.Request) (*http.Response, error) {
		sends.Add(1)
		entered <- struct{}{}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	prepared, err := s.PrepareReport(ctx, config, report.RangeOptions{}, serviceToday)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		results []DeliveryResult
		err     error
	}
	done := make(chan outcome, 1)
	go func() { results, err := s.SendPreparedReport(ctx, prepared); done <- outcome{results, err} }()
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("delivery did not reach concurrency limit")
		}
	}
	cancel()
	got := <-done
	if !errors.Is(got.err, context.Canceled) || len(got.results) != 5 {
		t.Fatalf("cancellation: %+v, %v", got.results, got.err)
	}
	counts := map[string]int{}
	for i, result := range got.results {
		counts[result.Status]++
		if result.Recipient != config.Recipients[i] || result.Error == "" {
			t.Errorf("result %d: %+v", i, result)
		}
	}
	if sends.Load() != 4 || counts["UNCERTAIN"] != 4 || counts["SKIPPED"] != 1 {
		t.Fatalf("sends=%d counts=%v", sends.Load(), counts)
	}
	if _, err := s.SendPreparedReport(context.Background(), prepared); !errors.Is(err, ErrPreparedReportUsed) {
		t.Fatalf("canceled batch reusable: %v", err)
	}
}

func TestServiceScheduledNoOpNeedsNoGoogleCredentials(t *testing.T) {
	_, config := serviceFixture(t)
	s := NewService(t.TempDir())
	if err := s.SaveSettings(config); err != nil {
		t.Fatal(err)
	}
	results, err := s.SendScheduledReport(context.Background(), serviceToday.AddDate(0, 1, 0))
	if err != nil || results != nil {
		t.Fatalf("outside schedule: %+v %v", results, err)
	}
	window, err := activeWindow(config, serviceToday)
	if err != nil {
		t.Fatal(err)
	}
	state, err := openDeliveryState(s.appDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.set(deliveryKey(window, config.TaskListID, config.Recipients[0].Address), DeliveryRecord{Status: "sent", MessageID: "prior-id"}); err != nil {
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	results, err = s.SendScheduledReport(context.Background(), serviceToday)
	if err != nil || len(results) != 1 || results[0].Status != "SKIPPED" || results[0].MessageID != "prior-id" {
		t.Fatalf("prior attempt: %+v %v", results, err)
	}
}
