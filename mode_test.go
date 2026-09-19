package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func writeTestCredentials(t *testing.T, dir string) {
	t.Helper()
	data := `{"installed":{"client_id":"test-client","client_secret":"test-secret","auth_uri":"https://example.test/auth","token_uri":"https://example.test/token","redirect_uris":["http://localhost"]}}`
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestInitModePreservesTokenOnFailedAuthorization(t *testing.T) {
	dir := t.TempDir()
	writeTestCredentials(t, dir)
	path := filepath.Join(dir, "token.json")
	original := []byte(`{"access_token":"old","refresh_token":"old-refresh"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	authorize := func(context.Context, *oauth2.Config) (*oauth2.Token, error) {
		called = true
		return nil, errors.New("denied")
	}
	err := runModeInDir(context.Background(), CLI{Init: true}, time.Now(), &bytes.Buffer{}, dir, authorize)
	if !called || err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("authorize called=%v error=%v", called, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("existing token was changed: %s", got)
	}
}

func TestInitModeSavesAuthorizedTokenWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	writeTestCredentials(t, dir)
	var output bytes.Buffer
	authorize := func(context.Context, *oauth2.Config) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "new-access", RefreshToken: "new-refresh", Expiry: time.Now().Add(time.Hour)}, nil
	}
	if err := runModeInDir(context.Background(), CLI{Init: true}, time.Now(), &output, dir, authorize); err != nil {
		t.Fatal(err)
	}
	token, err := loadToken(filepath.Join(dir, "token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "new-access" || token.RefreshToken != "new-refresh" {
		t.Errorf("saved token: %#v", token)
	}
	if !strings.Contains(output.String(), "authorization saved") {
		t.Errorf("output: %s", output.String())
	}
}

func TestListTaskListsModeWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	writeTestCredentials(t, dir)
	if err := saveTokenFile(filepath.Join(dir, "token.json"), &oauth2.Token{AccessToken: "access", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(req.URL.Path, "/tasks/v1/users/") {
			t.Errorf("unexpected request: %s", req.URL)
		}
		return jsonResponse(200, `{"items":[{"id":"google-id","title":"Work"}]}`), nil
	})}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	var output bytes.Buffer
	authorize := func(context.Context, *oauth2.Config) (*oauth2.Token, error) {
		t.Fatal("unexpected interactive authorization")
		return nil, nil
	}
	if err := runModeInDir(ctx, CLI{ListTaskLists: true}, time.Now(), &output, dir, authorize); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Errorf("requests=%d, want 1", requests)
	}
	if !strings.Contains(output.String(), "Work") || !strings.Contains(output.String(), "google-id") {
		t.Errorf("output: %s", output.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); !os.IsNotExist(err) {
		t.Errorf("unexpected config file: %v", err)
	}
}

func TestSavedRefreshFailureSuggestsInitInListAndReportModes(t *testing.T) {
	for _, cli := range []CLI{{ListTaskLists: true}, {}} {
		name := "list"
		if !cli.ListTaskLists {
			name = "report"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestCredentials(t, dir)
			expired := &oauth2.Token{AccessToken: "expired", RefreshToken: "revoked", Expiry: time.Now().Add(-time.Hour)}
			if err := saveTokenFile(filepath.Join(dir, "token.json"), expired); err != nil {
				t.Fatal(err)
			}
			if !cli.ListTaskLists {
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
			}
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Path != "/token" {
					t.Errorf("unexpected request: %s", req.URL)
				}
				return jsonResponse(400, `{"error":"invalid_grant"}`), nil
			})}
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
			authorize := func(context.Context, *oauth2.Config) (*oauth2.Token, error) {
				t.Fatal("unexpected interactive authorization")
				return nil, nil
			}
			err := runModeInDir(ctx, cli, time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC), &bytes.Buffer{}, dir, authorize)
			if err == nil || !strings.Contains(err.Error(), "weekly-report --init") {
				t.Errorf("error=%v", err)
			}
			if requests == 0 {
				t.Error("no token refresh was attempted")
			}
		})
	}
}
