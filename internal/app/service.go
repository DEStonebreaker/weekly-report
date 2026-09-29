package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
	tasksapi "google.golang.org/api/tasks/v1"

	"weekly-report/internal/google"
	"weekly-report/internal/report"
)

// Service coordinates application operations without terminal or Wails dependencies.
// Reuse one instance for a desktop session. It never authorizes interactively unless
// InitializeGoogle is explicitly called.
type Service struct{ appDir string }

func NewService(appDir string) *Service { return &Service{appDir: appDir} }

func NewDefaultService() (*Service, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("find user configuration directory: %w", err)
	}
	return NewService(filepath.Join(root, "weekly-tasks")), nil
}

func (s *Service) LoadSettings() (Config, error) {
	return loadConfig(filepath.Join(s.appDir, "config.toml"))
}

func (s *Service) LoadSettingsDraft() (Config, error) {
	return loadConfigDraft(filepath.Join(s.appDir, "config.toml"))
}

func (s *Service) CredentialsPath() string { return filepath.Join(s.appDir, "credentials.json") }

func (s *Service) HasSavedGoogleToken() bool {
	_, err := os.Stat(filepath.Join(s.appDir, "token.json"))
	return err == nil
}

func (s *Service) HasSettings() bool {
	_, err := os.Stat(filepath.Join(s.appDir, "config.toml"))
	return err == nil
}

func (s *Service) SaveSettings(config Config) error {
	return saveConfig(filepath.Join(s.appDir, "config.toml"), config)
}

func (s *Service) InitializeGoogle(ctx context.Context, authorize func(context.Context, *oauth2.Config) (*oauth2.Token, error)) error {
	config, err := google.LoadOAuthConfig(s.CredentialsPath())
	if err != nil {
		return err
	}
	if authorize == nil {
		return fmt.Errorf("Google authorization callback is required")
	}
	return google.InitializeToken(ctx, config, filepath.Join(s.appDir, "token.json"), authorize)
}

func (s *Service) googleClient(ctx context.Context) (*http.Client, error) {
	config, err := google.LoadOAuthConfig(s.CredentialsPath())
	if err != nil {
		return nil, err
	}
	return google.SavedGoogleClient(ctx, config, filepath.Join(s.appDir, "token.json"))
}

func listTaskLists(ctx context.Context, client *http.Client) (*tasksapi.Service, []google.TaskList, error) {
	tasks, err := google.CreateTasksService(ctx, client)
	if err != nil {
		return nil, nil, err
	}
	lists, err := google.GetTaskLists(ctx, tasks)
	if err != nil {
		return nil, nil, fmt.Errorf("get task lists: %w", google.WithAuthHint(err))
	}
	return tasks, lists, nil
}

func (s *Service) ListTaskLists(ctx context.Context) ([]google.TaskList, error) {
	client, err := s.googleClient(ctx)
	if err != nil {
		return nil, err
	}
	_, lists, err := listTaskLists(ctx, client)
	return lists, err
}

// PrepareReport reads tasks and prepares a manual report without sending email or
// consulting scheduled delivery state. Config may be an unsaved form snapshot.
func (s *Service) PrepareReport(ctx context.Context, config Config, options report.RangeOptions, now time.Time) (*PreparedReport, error) {
	config.Recipients = append([]Recipient(nil), config.Recipients...)
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}
	window, err := options.Resolve(now)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, err := s.googleClient(ctx)
	if err != nil {
		return nil, err
	}
	tasks, lists, err := listTaskLists(ctx, client)
	if err != nil {
		return nil, err
	}
	prepared, err := prepareReport(ctx, config, window, lists, tasks)
	if err != nil {
		return nil, err
	}
	prepared.owner = s
	return prepared, nil
}

// SendPreparedReport sends the exact prepared messages, without re-reading settings
// or Tasks. A snapshot is consumed when this operation starts, even if authorization
// fails. Cancellation before starting does not consume it. Never retry an uncertain
// batch automatically: inspect its results and deliberately prepare again if needed.
func (s *Service) SendPreparedReport(ctx context.Context, prepared *PreparedReport) ([]DeliveryResult, error) {
	return s.SendPreparedReportWithProgress(ctx, prepared, nil)
}

// SendPreparedReportWithProgress reports each completed recipient as the batch
// runs. A callback may be called concurrently and must be safe for concurrent use.
func (s *Service) SendPreparedReportWithProgress(ctx context.Context, prepared *PreparedReport, progress func(completed int, result DeliveryResult)) ([]DeliveryResult, error) {
	if prepared == nil || prepared.owner != s || prepared.attempted == nil || len(prepared.messages) == 0 {
		return nil, ErrInvalidPreparedReport
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !prepared.attempted.CompareAndSwap(false, true) {
		return nil, ErrPreparedReportUsed
	}
	client, err := s.googleClient(ctx)
	if err != nil {
		return skippedDeliveryResults(prepared.messages, err, progress), err
	}
	gmail, err := google.CreateGmailService(ctx, client)
	if err != nil {
		return skippedDeliveryResults(prepared.messages, err, progress), err
	}
	results := sendPreparedWithProgress(ctx, gmail, prepared.messages, nil, func(completed int, result sendResult) {
		if progress != nil {
			progress(completed, deliveryResults([]sendResult{result})[0])
		}
	})
	if err := ctx.Err(); err != nil {
		return deliveryResults(results), err
	}
	return deliveryResults(results), resultError(results)
}

func skippedDeliveryResults(messages []preparedEmail, cause error, progress func(int, DeliveryResult)) []DeliveryResult {
	results := make([]DeliveryResult, len(messages))
	for i, message := range messages {
		results[i] = DeliveryResult{Recipient: message.recipient, Status: "SKIPPED", Error: cause.Error()}
		if progress != nil {
			progress(i+1, results[i])
		}
	}
	return results
}

func (s *Service) SendManualReport(ctx context.Context, options report.RangeOptions, now time.Time) ([]DeliveryResult, error) {
	config, err := s.LoadSettings()
	if err != nil {
		return nil, err
	}
	prepared, err := s.PrepareReport(ctx, config, options, now)
	if err != nil {
		return nil, err
	}
	return s.SendPreparedReport(ctx, prepared)
}

// SendScheduledReport holds the process lock across preparation and delivery, and
// exits without network access outside the schedule or after all prior attempts.
func (s *Service) SendScheduledReport(ctx context.Context, now time.Time) ([]DeliveryResult, error) {
	config, err := s.LoadSettings()
	if err != nil {
		return nil, err
	}
	window, err := activeWindow(config, now)
	if err != nil {
		return nil, err
	}
	if window.Start.IsZero() {
		return nil, nil
	}
	state, err := openDeliveryState(s.appDir)
	if err != nil {
		return nil, err
	}
	defer state.Close()
	if results, complete := existingResults(config, window, state); complete {
		return deliveryResults(results), resultError(results)
	}
	client, err := s.googleClient(ctx)
	if err != nil {
		return nil, err
	}
	tasks, lists, err := listTaskLists(ctx, client)
	if err != nil {
		return nil, err
	}
	results, err := deliverReport(ctx, config, window, state, lists, tasks, client)
	return deliveryResults(results), err
}

// DeliveryResult preserves recipient order and can be serialized across a UI bridge.
// Error is text rather than a Go error interface, which would serialize as {}.
type DeliveryResult struct {
	Recipient Recipient `json:"recipient"`
	Status    string    `json:"status"` // SENT, FAILED, UNCERTAIN, or SKIPPED
	MessageID string    `json:"messageId,omitempty"`
	Error     string    `json:"error,omitempty"`
}

func deliveryResults(results []sendResult) []DeliveryResult {
	if results == nil {
		return nil
	}
	delivery := make([]DeliveryResult, len(results))
	for i, result := range results {
		delivery[i] = DeliveryResult{Recipient: result.recipient, Status: result.status, MessageID: result.messageID}
		if result.err != nil {
			delivery[i].Error = result.err.Error()
		}
	}
	return delivery
}

func selectTaskList(taskLists []google.TaskList, configuredID string) (string, error) {
	if configuredID == "" {
		return "", fmt.Errorf("task_list_id is empty; run --list-task-lists")
	}
	for _, taskList := range taskLists {
		if taskList.ID == configuredID {
			return taskList.ID, nil
		}
	}
	return "", fmt.Errorf("task list ID %q was not found; run --list-task-lists", configuredID)
}

func activeWindow(config Config, now time.Time) (report.ReportWindow, error) {
	return report.ActiveWindow(report.Schedule{StartDate: config.StartDate, EndDate: config.EndDate, WindowDays: config.WindowDays}, now)
}
