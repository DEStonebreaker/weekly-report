package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"weekly-report/internal/report"
)

func TestForceSendMidWindowRepeatAndNormalAfter(t *testing.T) {
	dir := t.TempDir()
	writeTestCredentials(t, dir)
	if err := saveTokenFile(filepath.Join(dir, "token.json"), &oauth2.Token{AccessToken: "access", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	config := `sender="sender@example.com"
subject="Report"
task_list_id="list-id"
start_date="2026-09-19"
end_date="2026-10-05"
window_days=7
[[recipients]]
name="Alice"
address="alice@example.com"
[[recipients]]
name="Bob"
address="bob@example.com"
[[recipients]]
name="Carol"
address="carol@example.com"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 14, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	window := report.ReportWindow{Start: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)}
	state, err := openDeliveryState(dir)
	if err != nil {
		t.Fatal(err)
	}
	for address, record := range map[string]DeliveryRecord{
		"alice@example.com": {Status: "sent", MessageID: "old-alice"},
		"bob@example.com":   {Status: "failed", Error: "old-bob"},
		"carol@example.com": {Status: "uncertain", Error: "old-carol"},
	} {
		if err := state.set(deliveryKey(window, "list-id", address), record); err != nil {
			t.Fatal(err)
		}
	}
	state.Close()
	var mu sync.Mutex
	sends := map[string]int{}
	taskCalls := 0
	listCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/tasks/v1/users/@me/lists":
			mu.Lock()
			listCalls++
			mu.Unlock()
			return jsonResponse(200, `{"items":[{"id":"list-id","title":"Work"}]}`), nil
		case "/tasks/v1/lists/list-id/tasks":
			mu.Lock()
			taskCalls++
			mu.Unlock()
			query := req.URL.Query()
			if got := query.Get("dueMin"); got != "2026-09-22T00:00:00Z" {
				t.Errorf("dueMin=%s", got)
			}
			if got := query.Get("dueMax"); got != "2026-09-25T23:59:59Z" {
				t.Errorf("dueMax=%s", got)
			}
			return jsonResponse(200, `{"items":[{"id":"task","title":"Due task","due":"2026-09-23T00:00:00Z"}]}`), nil
		case "/gmail/v1/users/me/messages/send":
			var body struct {
				Raw string `json:"raw"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decode Gmail request: %v", err)
			}
			decoded, err := base64.RawURLEncoding.DecodeString(body.Raw)
			if err != nil {
				t.Errorf("decode raw: %v", err)
			}
			text := string(decoded)
			if !strings.Contains(text, "Sep 22, 2026") || !strings.Contains(text, "Sep 25, 2026") {
				t.Errorf("wrong report range: %s", text)
			}
			var address string
			for _, candidate := range []string{"alice@example.com", "bob@example.com", "carol@example.com"} {
				if strings.Contains(text, candidate) {
					address = candidate
					break
				}
			}
			if address == "" {
				t.Errorf("recipient missing from message")
			}
			mu.Lock()
			sends[address]++
			count := sends[address]
			mu.Unlock()
			if count == 1 && address == "bob@example.com" {
				return jsonResponse(400, `{"error":{"code":400,"message":"denied"}}`), nil
			}
			if count == 1 && address == "carol@example.com" {
				return nil, errors.New("connection reset after request")
			}
			return jsonResponse(200, `{"id":"new-`+address+`"}`), nil
		default:
			t.Errorf("unexpected request %s", req.URL)
			return jsonResponse(404, `{}`), nil
		}
	})}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	authorize := func(context.Context, *oauth2.Config) (*oauth2.Token, error) {
		t.Fatal("unexpected OAuth prompt")
		return nil, nil
	}
	var output bytes.Buffer
	cli := Options{ForceSend: true}
	err = runModeInDir(ctx, cli, now, &output, dir, authorize)
	if err == nil {
		t.Fatal("force send with failures returned nil")
	}
	if !strings.Contains(output.String(), "1 sent, 1 failed, 1 uncertain, 0 skipped") {
		t.Errorf("first output: %s", output.String())
	}
	for _, address := range []string{"alice@example.com", "bob@example.com", "carol@example.com"} {
		if sends[address] != 1 {
			t.Errorf("first force sends[%s]=%d", address, sends[address])
		}
	}
	state, err = openDeliveryState(dir)
	if err != nil {
		t.Fatal(err)
	}
	for address, want := range map[string]string{"alice@example.com": "sent", "bob@example.com": "failed", "carol@example.com": "uncertain"} {
		record, ok := state.get(deliveryKey(window, "list-id", address))
		if !ok || record.Status != want || len(record.History) != 1 {
			t.Errorf("first state %s: %#v %v", address, record, ok)
		}
	}
	state.Close()
	output.Reset()
	if err := runModeInDir(ctx, cli, now, &output, dir, authorize); err != nil {
		t.Fatalf("repeat force: %v", err)
	}
	if !strings.Contains(output.String(), "3 sent, 0 failed, 0 uncertain, 0 skipped") {
		t.Errorf("repeat output: %s", output.String())
	}
	for _, address := range []string{"alice@example.com", "bob@example.com", "carol@example.com"} {
		if sends[address] != 2 {
			t.Errorf("repeat force sends[%s]=%d", address, sends[address])
		}
	}
	state, err = openDeliveryState(dir)
	if err != nil {
		t.Fatal(err)
	}
	for address, wantOld := range map[string]string{"alice@example.com": "sent", "bob@example.com": "failed", "carol@example.com": "uncertain"} {
		record, ok := state.get(deliveryKey(window, "list-id", address))
		if !ok || record.Status != "sent" || len(record.History) != 2 || record.History[0].Status != wantOld || record.History[1].AttemptedAt == "" || record.AttemptedAt == "" {
			t.Errorf("repeat state %s: %#v %v", address, record, ok)
		}
	}
	state.Close()
	mu.Lock()
	priorTasks, priorLists := taskCalls, listCalls
	mu.Unlock()
	output.Reset()
	if err := runModeInDir(ctx, Options{}, now, &output, dir, authorize); err != nil {
		t.Fatalf("normal after force: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if taskCalls != priorTasks || listCalls != priorLists {
		t.Errorf("normal invocation made API calls: tasks=%d lists=%d", taskCalls-priorTasks, listCalls-priorLists)
	}
	for _, address := range []string{"alice@example.com", "bob@example.com", "carol@example.com"} {
		if sends[address] != 2 {
			t.Errorf("normal sent to %s again", address)
		}
	}
}

func TestForceSendOutsideSchedule(t *testing.T) {
	dir := t.TempDir()
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
	authorize := func(context.Context, *oauth2.Config) (*oauth2.Token, error) {
		t.Fatal("unexpected OAuth")
		return nil, nil
	}
	cli := Options{ForceSend: true}
	for _, date := range []string{"2026-09-18", "2026-09-26"} {
		now, _ := time.Parse("2006-01-02", date)
		err := runModeInDir(context.Background(), cli, now, &bytes.Buffer{}, dir, authorize)
		if err == nil || !strings.Contains(err.Error(), "no active report window") {
			t.Errorf("%s: %v", date, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
		t.Errorf("state created outside schedule: %v", err)
	}
}
