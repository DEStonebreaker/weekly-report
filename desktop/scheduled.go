package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"weekly-report/internal/app"
	"weekly-report/internal/report"
)

func runScheduledRunner() int {
	service, err := app.NewDefaultService()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not initialize scheduled delivery: %v\n", err)
		return 1
	}
	base, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(base, 15*time.Minute)
	defer cancel()
	status, err := runScheduledDeliveryFrom(ctx, service, time.Now(), "launchd")
	if status != nil {
		printScheduledResults(status.Results)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "scheduled report run ended with an error: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "scheduled report run finished (%s)\n", status.Status)
	return 0
}

func printScheduledResults(results []app.DeliveryResult) {
	for _, result := range results {
		if result.Error != "" {
			fmt.Printf("%s\t%s\t%s\n", result.Status, result.Recipient.Address, result.Error)
		} else {
			fmt.Printf("%s\t%s\n", result.Status, result.Recipient.Address)
		}
	}
}

func (a *App) resumeScheduledDelivery(ctx context.Context) {
	settings, err := a.service.LoadSettings()
	if err != nil || !settings.ScheduleEnabled {
		return
	}
	active := systemScheduleActive(settings)
	if !active {
		active, err = applySystemSchedule(settings)
		if err != nil {
			status := &ScheduledRunStatus{Time: time.Now().Format(time.RFC3339), Status: "schedule-error", Source: "gui-catch-up", Error: err.Error()}
			_ = saveScheduledRun(status)
			wailsruntime.EventsEmit(ctx, "schedule-status", SchedulerStatusEvent{Active: false, Error: err.Error()})
			wailsruntime.EventsEmit(ctx, "scheduled-run-complete", status)
			return
		}
	}
	wailsruntime.EventsEmit(ctx, "schedule-status", SchedulerStatusEvent{Active: active})
	if !active || !shouldCatchUp(settings, time.Now()) {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(750 * time.Millisecond): // Let the setup screen subscribe to the result event.
	}
	status, _ := runScheduledDeliveryFrom(ctx, a.service, time.Now(), "gui-catch-up")
	if status != nil {
		wailsruntime.EventsEmit(ctx, "scheduled-run-complete", status)
	}
}

func runScheduledDelivery(ctx context.Context, service *app.Service, now time.Time) (*ScheduledRunStatus, error) {
	return runScheduledDeliveryFrom(ctx, service, now, "unknown")
}

func runScheduledDeliveryFrom(ctx context.Context, service *app.Service, now time.Time, source string) (*ScheduledRunStatus, error) {
	status, err := scheduledDelivery(ctx, service, now)
	status.Source = source
	if status.Status != "disabled" {
		if saveErr := saveScheduledRun(status); saveErr != nil && err == nil {
			err = fmt.Errorf("save scheduled run status: %w", saveErr)
			status.Status, status.Error = "error", err.Error()
		}
	}
	return status, err
}

// launchd may deliver yesterday's trigger before today's scheduled time after
// sleep. Only app-opening catch-up checks the clock (in shouldCatchUp); delivery
// still checks the enabled setting, active window, and prior recipient attempts.
func scheduledDelivery(ctx context.Context, service *app.Service, now time.Time) (*ScheduledRunStatus, error) {
	status := &ScheduledRunStatus{Time: now.Format(time.RFC3339), Status: "no-window"}
	settings, err := service.LoadSettings()
	if err != nil {
		status.Status, status.Error = "error", err.Error()
		return status, err
	}
	if !settings.ScheduleEnabled {
		status.Status = "disabled"
		return status, nil
	}
	outcome, err := service.SendScheduledReportDetailed(ctx, now)
	status.Results = outcome.Results
	if outcome.TasksQueried {
		status.TaskCount = &outcome.TaskCount
	}
	if !outcome.Window.Start.IsZero() {
		status.StartDate = outcome.Window.Start.Format("2006-01-02")
		status.EndDate = outcome.Window.End.Format("2006-01-02")
	}
	if err != nil {
		status.Status, status.Error = "error", err.Error()
	} else if outcome.NoTasks {
		status.Status = "no-tasks"
	} else if len(status.Results) > 0 {
		status.Status = "already-sent"
		for _, result := range status.Results {
			if result.Status != "SKIPPED" {
				status.Status = "complete"
				break
			}
		}
	}
	return status, err
}

func scheduleTimePassed(value string, now time.Time) bool {
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return false
	}
	local := now
	trigger := time.Date(local.Year(), local.Month(), local.Day(), parsed.Hour(), parsed.Minute(), 0, 0, local.Location())
	return !local.Before(trigger)
}

func shouldCatchUp(settings app.Config, now time.Time) bool {
	if !settings.ScheduleEnabled || !scheduleTimePassed(settings.ScheduleTime, now) {
		return false
	}
	window, err := report.ActiveWindow(report.Schedule{StartDate: settings.StartDate, EndDate: settings.EndDate, WindowDays: settings.WindowDays}, now)
	return err == nil && !window.Start.IsZero()
}

func loadScheduledRun() (*ScheduledRunStatus, error) {
	path, err := scheduledRunPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var status ScheduledRunStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func saveScheduledRun(status *ScheduledRunStatus) error {
	path, err := scheduledRunPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".scheduled-run-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func scheduledRunPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "weekly-tasks", "last-scheduled-run.json"), nil
}
