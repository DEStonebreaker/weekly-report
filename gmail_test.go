package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
	"time"

	gmailapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func TestBuildRawMessage(t *testing.T) {
	sentAt := time.Date(2026, time.September, 17, 9, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	email := OutgoingEmail{
		From:     "Donovan Sender <sender@example.com>",
		To:       "Weekly Report Recipient <recipient@example.com>",
		Subject:  "Weekly résumé ✓",
		HTMLBody: "<!doctype html><html><body><p>Café &amp; tasks ✓</p></body></html>",
	}

	raw, err := buildRawMessage(email, sentAt)
	if err != nil {
		t.Fatalf("buildRawMessage() error = %v", err)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw Gmail message: %v", err)
	}

	message, err := mail.ReadMessage(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("parse decoded email: %v", err)
	}

	wantFrom, err := mail.ParseAddress(email.From)
	if err != nil {
		t.Fatalf("parse expected sender: %v", err)
	}
	if got, want := message.Header.Get("From"), wantFrom.String(); got != want {
		t.Errorf("From header = %q, want %q", got, want)
	}

	wantTo, err := mail.ParseAddress(email.To)
	if err != nil {
		t.Fatalf("parse expected recipient: %v", err)
	}
	if got, want := message.Header.Get("To"), wantTo.String(); got != want {
		t.Errorf("To header = %q, want %q", got, want)
	}

	decodedSubject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode Subject header: %v", err)
	}
	if decodedSubject != email.Subject {
		t.Errorf("Subject header = %q, want %q", decodedSubject, email.Subject)
	}

	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type header: %v", err)
	}
	if mediaType != "text/html" {
		t.Errorf("Content-Type media type = %q, want %q", mediaType, "text/html")
	}
	if got := strings.ToLower(params["charset"]); got != "utf-8" {
		t.Errorf("Content-Type charset = %q, want %q", got, "utf-8")
	}

	if got, want := message.Header.Get("Date"), sentAt.Format(time.RFC1123Z); got != want {
		t.Errorf("Date header = %q, want %q", got, want)
	}
	if got := message.Header.Get("MIME-Version"); got != "1.0" {
		t.Errorf("MIME-Version header = %q, want %q", got, "1.0")
	}
	if got := message.Header.Get("Content-Transfer-Encoding"); got != "quoted-printable" {
		t.Errorf("Content-Transfer-Encoding header = %q, want %q", got, "quoted-printable")
	}

	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatalf("decode quoted-printable body: %v", err)
	}
	if got := string(body); got != email.HTMLBody {
		t.Errorf("decoded HTML body = %q, want %q", got, email.HTMLBody)
	}
}

func TestBuildRawMessageRejectsInvalidEmail(t *testing.T) {
	validEmail := OutgoingEmail{
		From:     "sender@example.com",
		To:       "recipient@example.com",
		Subject:  "Weekly report",
		HTMLBody: "<p>Weekly report</p>",
	}

	tests := []struct {
		name        string
		modify      func(*OutgoingEmail)
		wantErrText string
	}{
		{
			name:        "empty sender",
			modify:      func(email *OutgoingEmail) { email.From = "" },
			wantErrText: "parse email sender",
		},
		{
			name:        "malformed sender",
			modify:      func(email *OutgoingEmail) { email.From = "not-an-address" },
			wantErrText: "parse email sender",
		},
		{
			name:        "sender header injection",
			modify:      func(email *OutgoingEmail) { email.From = "sender@example.com\r\nBcc: attacker@example.com" },
			wantErrText: "parse email sender",
		},
		{
			name:        "empty recipient",
			modify:      func(email *OutgoingEmail) { email.To = "" },
			wantErrText: "parse email recipient",
		},
		{
			name:        "malformed recipient",
			modify:      func(email *OutgoingEmail) { email.To = "not-an-address" },
			wantErrText: "parse email recipient",
		},
		{
			name:        "recipient header injection",
			modify:      func(email *OutgoingEmail) { email.To = "recipient@example.com\r\nBcc: attacker@example.com" },
			wantErrText: "parse email recipient",
		},
		{
			name:        "subject carriage return",
			modify:      func(email *OutgoingEmail) { email.Subject = "Weekly\rBcc: attacker@example.com" },
			wantErrText: "email subject contains invalid characters",
		},
		{
			name:        "subject newline",
			modify:      func(email *OutgoingEmail) { email.Subject = "Weekly\nBcc: attacker@example.com" },
			wantErrText: "email subject contains invalid characters",
		},
		{
			name:        "empty HTML body",
			modify:      func(email *OutgoingEmail) { email.HTMLBody = "" },
			wantErrText: "email HTML body is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := validEmail
			tt.modify(&email)

			_, err := buildRawMessage(email, time.Time{})
			if err == nil {
				t.Fatal("buildRawMessage() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("buildRawMessage() error = %q, want it to contain %q", err, tt.wantErrText)
			}
		})
	}
}

func TestSendEmail(t *testing.T) {
	type capturedRequest struct {
		method  string
		path    string
		message gmailapi.Message
		err     error
	}

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request gmailapi.Message
		err := json.NewDecoder(r.Body).Decode(&request)
		requests <- capturedRequest{
			method:  r.Method,
			path:    r.URL.Path,
			message: request,
			err:     err,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(&gmailapi.Message{
			Id:       "test-message-id",
			ThreadId: "test-thread-id",
		}); err != nil {
			t.Errorf("encode fake Gmail response: %v", err)
		}
	}))
	defer server.Close()

	service := newTestGmailService(t, server)
	email := OutgoingEmail{
		From:     "Donovan Sender <sender@example.com>",
		To:       "Weekly Report Recipient <recipient@example.com>",
		Subject:  "Weekly résumé ✓",
		HTMLBody: "<p>Café &amp; tasks ✓</p>",
	}

	messageID, err := SendEmail(context.Background(), service, email)
	if err != nil {
		t.Fatalf("SendEmail() error = %v", err)
	}
	if messageID != "test-message-id" {
		t.Errorf("SendEmail() message ID = %q, want %q", messageID, "test-message-id")
	}

	request := <-requests
	if request.err != nil {
		t.Fatalf("decode Gmail API request: %v", request.err)
	}
	if request.method != http.MethodPost {
		t.Errorf("request method = %q, want %q", request.method, http.MethodPost)
	}
	if want := "/gmail/v1/users/me/messages/send"; request.path != want {
		t.Errorf("request path = %q, want %q", request.path, want)
	}
	if request.message.Raw == "" {
		t.Fatal("request raw message is empty")
	}

	assertRawEmail(t, request.message.Raw, email)
}

func TestSendEmailReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := io.WriteString(w, `{"error":{"code":500,"message":"test failure","status":"INTERNAL"}}`); err != nil {
			t.Errorf("write fake Gmail error response: %v", err)
		}
	}))
	defer server.Close()

	service := newTestGmailService(t, server)
	_, err := SendEmail(context.Background(), service, OutgoingEmail{
		From:     "sender@example.com",
		To:       "recipient@example.com",
		Subject:  "Weekly report",
		HTMLBody: "<p>Weekly report</p>",
	})
	if err == nil {
		t.Fatal("SendEmail() error = nil, want an API error")
	}
	if !strings.Contains(err.Error(), "send Gmail message") {
		t.Errorf("SendEmail() error = %q, want it to contain %q", err, "send Gmail message")
	}
}

func newTestGmailService(t *testing.T, server *httptest.Server) *gmailapi.Service {
	t.Helper()

	service, err := gmailapi.NewService(
		context.Background(),
		option.WithHTTPClient(server.Client()),
		option.WithEndpoint(server.URL+"/"),
	)
	if err != nil {
		t.Fatalf("create test Gmail service: %v", err)
	}

	return service
}

func assertRawEmail(t *testing.T, raw string, email OutgoingEmail) {
	t.Helper()

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw Gmail message: %v", err)
	}

	message, err := mail.ReadMessage(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("parse decoded email: %v", err)
	}

	wantFrom, err := mail.ParseAddress(email.From)
	if err != nil {
		t.Fatalf("parse expected sender: %v", err)
	}
	if got, want := message.Header.Get("From"), wantFrom.String(); got != want {
		t.Errorf("From header = %q, want %q", got, want)
	}

	wantTo, err := mail.ParseAddress(email.To)
	if err != nil {
		t.Fatalf("parse expected recipient: %v", err)
	}
	if got, want := message.Header.Get("To"), wantTo.String(); got != want {
		t.Errorf("To header = %q, want %q", got, want)
	}

	decodedSubject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode Subject header: %v", err)
	}
	if decodedSubject != email.Subject {
		t.Errorf("Subject header = %q, want %q", decodedSubject, email.Subject)
	}

	mediaType, _, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type header: %v", err)
	}
	if mediaType != "text/html" {
		t.Errorf("Content-Type media type = %q, want %q", mediaType, "text/html")
	}

	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatalf("decode quoted-printable body: %v", err)
	}
	if got := string(body); got != email.HTMLBody {
		t.Errorf("decoded HTML body = %q, want %q", got, email.HTMLBody)
	}
}
