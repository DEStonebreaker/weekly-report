package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gmailapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func TestSendPreparedOverlapsAndPersistsResults(t *testing.T) {
	var active, peak atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		current := active.Add(1)
		for {
			old := peak.Load()
			if current <= old || peak.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		var request gmailapi.Message
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Raw == "" {
			t.Error("empty raw message")
		}
		decoded, err := base64.RawURLEncoding.DecodeString(request.Raw)
		if err != nil {
			t.Errorf("decode raw message: %v", err)
		}
		if strings.Contains(string(decoded), "bob@example.com") {
			return jsonResponse(400, `{"error":{"code":400,"message":"failure"}}`), nil
		}
		return jsonResponse(200, `{"id":"message-id"}`), nil
	})}
	service, err := gmailapi.NewService(context.Background(), option.WithHTTPClient(client), option.WithEndpoint("https://example.test/"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := openDeliveryState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	window := ReportWindow{Start: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)}
	prepared := make([]preparedEmail, 2)
	for i, address := range []string{"alice@example.com", "bob@example.com"} {
		raw, err := buildRawMessage(OutgoingEmail{From: "sender@example.com", To: address, Subject: "Report", HTMLBody: "<p>Hi</p>"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		prepared[i] = preparedEmail{recipient: Recipient{Address: address}, key: deliveryKey(window, "list", address), raw: raw}
	}
	done := make(chan []sendResult, 1)
	go func() { done <- sendPrepared(context.Background(), service, prepared, state) }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatal("sends did not overlap")
		}
	}
	close(release)
	results := <-done
	if peak.Load() < 2 {
		t.Errorf("peak concurrency=%d", peak.Load())
	}
	for i, result := range results {
		want := "SENT"
		if i == 1 {
			want = "FAILED"
		}
		if result.status != want {
			t.Errorf("result %d: %#v", i, result)
		}
		wantState := "sent"
		if i == 1 {
			wantState = "failed"
		}
		if record, ok := state.get(prepared[i].key); !ok || record.Status != wantState {
			t.Errorf("state %d: %#v, %v", i, record, ok)
		}
	}
}

func TestDeliveryStateLocksAndRetainsUncertainAttempt(t *testing.T) {
	dir := t.TempDir()
	first, err := openDeliveryState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openDeliveryState(dir); err == nil {
		t.Error("overlapping process was not rejected")
	}
	key := "window/list/recipient"
	if err := first.set(key, DeliveryRecord{Status: "uncertain"}); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := openDeliveryState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if record, ok := second.get(key); !ok || record.Status != "uncertain" {
		t.Errorf("record=%#v, %v", record, ok)
	}
}

func TestTransportFailureStaysUncertainAndIsNotRetried(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("connection reset after request")
	})}
	service, err := gmailapi.NewService(context.Background(), option.WithHTTPClient(client), option.WithEndpoint("https://example.test/"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := openDeliveryState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	window := ReportWindow{Start: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)}
	recipient := Recipient{Name: "Alice", Address: "alice@example.com"}
	config := Config{TaskListID: "list-id", Recipients: []Recipient{recipient}}
	key := deliveryKey(window, config.TaskListID, recipient.Address)
	raw, err := buildRawMessage(OutgoingEmail{From: "sender@example.com", To: recipient.Address, Subject: "Report", HTMLBody: "<p>Hi</p>"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	result := sendPrepared(context.Background(), service, []preparedEmail{{recipient: recipient, key: key, raw: raw}}, state)
	if result[0].status != "UNCERTAIN" {
		t.Errorf("result=%#v", result[0])
	}
	record, ok := state.get(key)
	if !ok || record.Status != "uncertain" {
		t.Errorf("record=%#v, %v", record, ok)
	}
	repeated, complete := existingResults(config, window, state)
	if !complete || repeated[0].status != "UNCERTAIN" {
		t.Errorf("repeated=%#v complete=%v", repeated, complete)
	}
	if calls != 1 {
		t.Errorf("Gmail calls=%d, want 1", calls)
	}
	var output bytes.Buffer
	printSendResults(&output, repeated)
	if !strings.Contains(output.String(), "UNCERTAIN") || !strings.Contains(output.String(), "1 uncertain") {
		t.Errorf("output=%s", output.String())
	}
}
