package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/oauth2"

	"weekly-report/internal/app"
	"weekly-report/internal/google"
	"weekly-report/internal/report"
)

// App is the Wails adapter. Google tokens and OAuth internals remain in Go.
type App struct {
	ctx        context.Context
	service    *app.Service
	mu         sync.Mutex
	prepared   *app.PreparedReport
	preparedID string
	revision   uint64
	prepareSeq uint64
	sending    bool
	connecting bool
}

type DesktopInfo struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
}

type SetupData struct {
	Settings         app.Config          `json:"settings"`
	SettingsSaved    bool                `json:"settingsSaved"`
	GoogleConnected  bool                `json:"googleConnected"`
	CredentialsPath  string              `json:"credentialsPath"`
	SchedulerActive  bool                `json:"schedulerActive"`
	LastScheduledRun *ScheduledRunStatus `json:"lastScheduledRun,omitempty"`
}

type SettingsSaveResult struct {
	Saved           bool   `json:"saved"`
	SchedulerActive bool   `json:"schedulerActive"`
	ScheduleError   string `json:"scheduleError,omitempty"`
}

type SchedulerStatusEvent struct {
	Active bool   `json:"active"`
	Error  string `json:"error,omitempty"`
}

type PreparedPreview struct {
	ID      string            `json:"id"`
	Preview app.ReportPreview `json:"preview"`
}

type ProgressEvent struct {
	PreparationID string              `json:"preparationId"`
	Completed     int                 `json:"completed"`
	Total         int                 `json:"total"`
	Result        *app.DeliveryResult `json:"result,omitempty"`
}

type SendOutcome struct {
	Results []app.DeliveryResult `json:"results"`
	Error   string               `json:"error,omitempty"`
}

type ScheduledRunStatus struct {
	Time    string               `json:"time"`
	Status  string               `json:"status"`
	Results []app.DeliveryResult `json:"results,omitempty"`
	Error   string               `json:"error,omitempty"`
}

func NewApp() (*App, error) {
	service, err := app.NewDefaultService()
	if err != nil {
		return nil, err
	}
	return &App{service: service}, nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.resumeScheduledDelivery(ctx)
}

func (a *App) GetDesktopInfo() DesktopInfo {
	return DesktopInfo{Name: "Weekly Report", Platform: goruntime.GOOS, Arch: goruntime.GOARCH}
}

func (a *App) GetSetupData() (SetupData, error) {
	settings, err := a.service.LoadSettingsDraft()
	if err != nil {
		if a.service.HasSettings() {
			return SetupData{}, fmt.Errorf("could not read saved settings: %w", err)
		}
		today := time.Now()
		start := today.Format("2006-01-02")
		end := today.AddDate(0, 0, 6).Format("2006-01-02")
		settings = app.Config{Subject: "Weekly Report", StartDate: start, EndDate: end, WindowDays: 7}
	}
	if settings.ScheduleTime == "" {
		settings.ScheduleTime = "09:00"
	}
	if settings.Recipients == nil {
		settings.Recipients = []app.Recipient{}
	}
	lastRun, _ := loadScheduledRun()
	return SetupData{
		Settings: settings, SettingsSaved: a.service.HasSettings(),
		GoogleConnected: a.service.HasSavedGoogleToken(), CredentialsPath: a.service.CredentialsPath(),
		SchedulerActive: systemScheduleActive(settings), LastScheduledRun: lastRun,
	}, nil
}

func (a *App) ConnectGoogle() error {
	return a.connectGoogle(func(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
		return google.AuthorizeWithGoogleBrowser(ctx, config, func(url string) error {
			wailsruntime.BrowserOpenURL(a.ctx, url)
			return nil
		})
	})
}

func (a *App) connectGoogle(authorize func(context.Context, *oauth2.Config) (*oauth2.Token, error)) error {
	if a.ctx == nil {
		return fmt.Errorf("desktop application is still starting; try again")
	}
	a.mu.Lock()
	if a.sending || a.connecting {
		a.mu.Unlock()
		return fmt.Errorf("wait for the current send or Google connection to finish")
	}
	a.connecting = true
	a.prepareSeq++ // Reject preparations already in flight against the old connection.
	a.prepared = nil
	a.preparedID = ""
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.connecting = false
		a.mu.Unlock()
	}()
	err := a.service.InitializeGoogle(a.ctx, authorize)
	if err != nil {
		return fmt.Errorf("Google connection failed: %w", err)
	}
	return nil
}

func (a *App) ListTaskLists() ([]google.TaskList, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("desktop application is still starting; try again")
	}
	lists, err := a.service.ListTaskLists(a.ctx)
	if err != nil {
		return nil, fmt.Errorf("could not load Google task lists: %s", desktopGoogleError(err))
	}
	return lists, nil
}

func desktopGoogleError(err error) string {
	message := err.Error()
	message = strings.ReplaceAll(message, "run weekly-report --init", "use Reconnect Google below")
	return message
}

func (a *App) SaveSettings(settings app.Config) (SettingsSaveResult, error) {
	if err := a.service.SaveSettings(settings); err != nil {
		return SettingsSaveResult{}, fmt.Errorf("settings were not saved: %w", err)
	}
	active, err := applySystemSchedule(settings)
	result := SettingsSaveResult{Saved: true, SchedulerActive: active}
	if err != nil {
		result.ScheduleError = err.Error()
		result.SchedulerActive = false
	}
	return result, nil
}

// InvalidateReportPreparation drops any preview made before the latest form edit.
// The revision suppresses late bridge calls arriving out of order.
func (a *App) InvalidateReportPreparation(revision uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sending || revision <= a.revision {
		return
	}
	a.revision = revision
	a.prepareSeq++
	a.prepared = nil
	a.preparedID = ""
}

func (a *App) PrepareReport(settings app.Config, preset, from, to string, revision uint64) (PreparedPreview, error) {
	options, err := manualRange(preset, from, to)
	if err != nil {
		return PreparedPreview{}, err
	}
	return a.prepareReport(revision, func() (*app.PreparedReport, error) {
		return a.service.PrepareReport(a.ctx, settings, options, time.Now())
	})
}

// PrepareForceSend previews the selected dates with saved settings through the
// manual delivery path, leaving scheduled recipient attempts untouched.
func (a *App) PrepareForceSend(preset, from, to string, revision uint64) (PreparedPreview, error) {
	return a.prepareForceSendAt(preset, from, to, revision, time.Now())
}

func (a *App) prepareForceSendAt(preset, from, to string, revision uint64, now time.Time) (PreparedPreview, error) {
	options, err := manualRange(preset, from, to)
	if err != nil {
		return PreparedPreview{}, err
	}
	return a.prepareReport(revision, func() (*app.PreparedReport, error) {
		settings, err := a.service.LoadSettings()
		if err != nil {
			return nil, fmt.Errorf("could not load saved settings: %w", err)
		}
		return a.service.PrepareReport(a.ctx, settings, options, now)
	})
}

func (a *App) prepareReport(revision uint64, build func() (*app.PreparedReport, error)) (PreparedPreview, error) {
	if a.ctx == nil {
		return PreparedPreview{}, fmt.Errorf("desktop application is still starting; try again")
	}
	a.mu.Lock()
	if a.sending || a.connecting {
		a.mu.Unlock()
		return PreparedPreview{}, fmt.Errorf("wait for the current send or Google connection to finish")
	}
	if revision < a.revision {
		a.mu.Unlock()
		return PreparedPreview{}, fmt.Errorf("report inputs changed; prepare the report again")
	}
	if revision > a.revision {
		a.revision = revision
	}
	a.prepareSeq++
	seq := a.prepareSeq
	a.prepared = nil
	a.preparedID = ""
	a.mu.Unlock()

	prepared, err := build()
	if err != nil {
		return PreparedPreview{}, fmt.Errorf("could not prepare report: %s", desktopGoogleError(err))
	}
	id, err := newPreparationID()
	if err != nil {
		return PreparedPreview{}, fmt.Errorf("could not create report preview: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if seq != a.prepareSeq || revision != a.revision || a.sending {
		return PreparedPreview{}, fmt.Errorf("report inputs changed while preparing; prepare again")
	}
	a.prepared = prepared
	a.preparedID = id
	return PreparedPreview{ID: id, Preview: prepared.Preview()}, nil
}

func (a *App) DiscardReportPreparation(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.sending && id != "" && a.preparedID == id {
		a.prepared = nil
		a.preparedID = ""
	}
}

func (a *App) SendPreparedReport(id string) SendOutcome {
	a.mu.Lock()
	if a.sending || a.connecting {
		a.mu.Unlock()
		return SendOutcome{Error: "Wait for the current send or Google connection to finish."}
	}
	if id == "" || a.prepared == nil || a.preparedID != id {
		a.mu.Unlock()
		return SendOutcome{Error: "This preview has expired. Prepare the report again before sending."}
	}
	prepared := a.prepared
	total := len(prepared.Preview().Messages)
	a.prepared = nil
	a.preparedID = ""
	a.sending = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.sending = false
		a.mu.Unlock()
	}()

	if a.ctx == nil {
		return SendOutcome{Error: "Desktop application is closing. Reopen it before sending."}
	}
	wailsruntime.EventsEmit(a.ctx, "delivery-progress", ProgressEvent{PreparationID: id, Total: total})
	results, err := a.service.SendPreparedReportWithProgress(a.ctx, prepared, func(completed int, result app.DeliveryResult) {
		copy := result
		wailsruntime.EventsEmit(a.ctx, "delivery-progress", ProgressEvent{PreparationID: id, Completed: completed, Total: total, Result: &copy})
	})
	outcome := SendOutcome{Results: results}
	if err != nil {
		outcome.Error = desktopGoogleError(err)
	}
	return outcome
}

func newPreparationID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func manualRange(preset, from, to string) (report.RangeOptions, error) {
	switch preset {
	case "next7":
		days := 7
		return report.RangeOptions{Days: &days}, nil
	case "next14":
		days := 14
		return report.RangeOptions{Days: &days}, nil
	case "thisWeek":
		return report.RangeOptions{ThisWeek: true}, nil
	case "nextWeek":
		return report.RangeOptions{NextWeek: true}, nil
	case "custom":
		options := report.RangeOptions{From: from, To: to}
		if err := options.Validate(); err != nil {
			return report.RangeOptions{}, fmt.Errorf("custom date range: %w", err)
		}
		return options, nil
	default:
		return report.RangeOptions{}, fmt.Errorf("choose a report date range")
	}
}
