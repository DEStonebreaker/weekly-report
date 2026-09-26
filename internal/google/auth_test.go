package google

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestAuthorizeWithGoogle(t *testing.T) {
	// AuthorizeWithGoogle prints the URL for the user. Act as that browser,
	// while keeping Google token exchange entirely local.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = originalStdout; writer.Close(); reader.Close() }()

	var authQuery url.Values
	exchange := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("redirect_uri") != authQuery.Get("redirect_uri") {
			t.Error("exchange redirect does not match authorization redirect")
		}
		if r.Form.Get("code") != "test-code" {
			t.Errorf("unexpected code: %q", r.Form.Get("code"))
		}
		verifier := r.Form.Get("code_verifier")
		challenge := sha256.Sum256([]byte(verifier))
		if verifier == "" || base64.RawURLEncoding.EncodeToString(challenge[:]) != authQuery.Get("code_challenge") {
			t.Error("PKCE verifier does not match challenge")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`)
	}))
	defer exchange.Close()
	config := &oauth2.Config{ClientID: "client", RedirectURL: "http://original", Endpoint: oauth2.Endpoint{AuthURL: "https://example.com/auth", TokenURL: exchange.URL, AuthStyle: oauth2.AuthStyleInParams}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type outcome struct {
		token *oauth2.Token
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		token, err := AuthorizeWithGoogle(ctx, config)
		done <- outcome{token, err}
		writer.Close()
	}()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if !strings.HasPrefix(scanner.Text(), "https://example.com/auth?") {
			continue
		}
		authURL, err := url.Parse(scanner.Text())
		if err != nil {
			t.Fatal(err)
		}
		authQuery = authURL.Query()
		break
	}
	if authQuery == nil {
		t.Fatal("no authorization URL printed")
	}
	if authQuery.Get("state") == "" || authQuery.Get("code_challenge_method") != "S256" || authQuery.Get("access_type") != "offline" {
		t.Fatalf("authorization parameters = %v", authQuery)
	}
	redirect, err := url.Parse(authQuery.Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	if redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" || redirect.Path != "/callback" {
		t.Fatalf("callback URL = %s", redirect)
	}
	redirect.RawQuery = url.Values{"state": {authQuery.Get("state")}, "code": {"test-code"}}.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, redirect.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "return to the terminal") {
		t.Fatalf("callback response: %s, %v", body, err)
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.token.AccessToken != "access" || result.token.RefreshToken != "refresh" {
		t.Fatalf("token = %#v", result.token)
	}
	if config.RedirectURL != "http://original" {
		t.Fatalf("original config was mutated: %s", config.RedirectURL)
	}
	assertCallbackClosed(t, authQuery.Get("redirect_uri"))
}
