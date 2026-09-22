package google

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"strings"
	"time"

	gmailapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type OutgoingEmail struct {
	From     string
	To       string
	Subject  string
	HTMLBody string
}

func CreateGmailService(
	ctx context.Context,
	client *http.Client,
) (*gmailapi.Service, error) {
	service, err := gmailapi.NewService(
		ctx,
		option.WithHTTPClient(client),
	)
	if err != nil {
		return nil, fmt.Errorf("create Gmail service: %w", err)
	}

	return service, nil
}

func BuildRawMessage(email OutgoingEmail, sentAt time.Time) (string, error) {
	parsedFrom, err := mail.ParseAddress(email.From)
	if err != nil {
		return "", fmt.Errorf("parse email sender: %w", err)
	}

	parsedTo, err := mail.ParseAddress(email.To)
	if err != nil {
		return "", fmt.Errorf("parse email recipient: %w", err)
	}

	if strings.Contains(email.Subject, "\r") ||
		strings.Contains(email.Subject, "\n") {
		return "", fmt.Errorf("email subject contains invalid characters")
	}

	subject := mime.QEncoding.Encode("utf-8", email.Subject)

	if email.HTMLBody == "" {
		return "", fmt.Errorf("email HTML body is empty")
	}

	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", parsedFrom.String())
	fmt.Fprintf(&message, "To: %s\r\n", parsedTo.String())
	fmt.Fprintf(&message, "Subject: %s\r\n", subject)
	fmt.Fprintf(&message, "Date: %s\r\n", sentAt.Format(time.RFC1123Z))
	message.WriteString("MIME-Version: 1.0\r\n")
	message.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	message.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")

	bodyWriter := quotedprintable.NewWriter(&message)
	if _, err := bodyWriter.Write([]byte(email.HTMLBody)); err != nil {
		return "", fmt.Errorf("write email HTML body: %w", err)
	}

	if err := bodyWriter.Close(); err != nil {
		return "", fmt.Errorf("close quoted-printable writer: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(message.Bytes()), nil
}

func SendEmail(
	ctx context.Context,
	service *gmailapi.Service,
	email OutgoingEmail,
) (string, error) {
	raw, err := BuildRawMessage(email, time.Now())
	if err != nil {
		return "", err
	}
	return SendRawEmail(ctx, service, raw)
}

func SendRawEmail(ctx context.Context, service *gmailapi.Service, raw string) (string, error) {
	sent, err := service.Users.Messages.Send(
		"me",
		&gmailapi.Message{Raw: raw},
	).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("send Gmail message: %w", err)
	}

	return sent.Id, nil
}

// DeliveryUncertain reports whether Gmail may have accepted a failed request.
func DeliveryUncertain(err error) bool {
	var apiErr *googleapi.Error
	return !errors.As(err, &apiErr) || apiErr.Code >= 500
}
