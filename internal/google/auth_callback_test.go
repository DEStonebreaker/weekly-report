package google

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestReceiveAuthorizationCode(t *testing.T) {
	for _, denied := range []bool{false, true} {
		name := "success"
		if denied {
			name = "denied"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var callbackURL string
			code, err := receiveAuthorizationCode(ctx, "expected", func(redirect string) error {
				callbackURL = redirect
				for _, request := range []struct {
					method, query string
					status        int
				}{
					{"POST", "state=expected&code=code", http.StatusMethodNotAllowed},
					{"GET", "state=wrong&code=code", http.StatusBadRequest},
					{"GET", "state=wrong&error=access_denied", http.StatusBadRequest},
					{"GET", "state=expected", http.StatusBadRequest},
				} {
					req, _ := http.NewRequestWithContext(ctx, request.method, redirect+"?"+request.query, nil)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode != request.status {
						t.Fatalf("status = %d, want %d", resp.StatusCode, request.status)
					}
				}
				query := url.Values{"state": {"expected"}, "code": {"code"}}
				if denied {
					query = url.Values{"state": {"expected"}, "error": {"access_denied"}}
				}
				// Keep the waiter paused until duplicate requests have completed.
				for range 2 {
					req, _ := http.NewRequestWithContext(ctx, "GET", redirect+"?"+query.Encode(), nil)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil || resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "return to the terminal") {
						t.Fatalf("callback response: %s, %v", body, err)
					}
				}
				return nil
			}, "return to the terminal")
			if denied {
				if err == nil || !strings.Contains(err.Error(), "access_denied") {
					t.Fatalf("error = %v", err)
				}
			} else if err != nil || code != "code" {
				t.Fatalf("code = %q, error = %v", code, err)
			}
			assertCallbackClosed(t, callbackURL)
		})
	}
}

func TestReceiveAuthorizationCodeCancellation(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		name := "cancel"
		if timeout {
			name = "timeout"
		}
		t.Run(name, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			want := context.Canceled
			if timeout {
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
				want = context.DeadlineExceeded
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			var callbackURL string
			_, err := receiveAuthorizationCode(ctx, "expected", func(redirect string) error {
				callbackURL = redirect
				if !timeout {
					cancel()
				}
				return nil
			}, "return to the terminal")
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			assertCallbackClosed(t, callbackURL)
		})
	}
}

func assertCallbackClosed(t *testing.T, callbackURL string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(callbackURL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("callback listener still accepts requests")
	}
}
