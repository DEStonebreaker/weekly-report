package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	appConfig := Config{
		Sender:     "sample@gmail.com",
		Subject:    "[TEST] Weekly Report",
		WindowDays: 7,
		Recipients: []Recipient{
			{Name: "Donovan", Address: "blha@gmail.com"},
		},
	}
	if err := appConfig.Validate(); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}

	configRoot, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("find user configuration directory: %w", err)
	}

	appDir := filepath.Join(configRoot, "weekly-tasks")
	credentialsPath := filepath.Join(appDir, "credentials.json")
	tokenPath := filepath.Join(appDir, "token.json")

	oauthConfig, err := loadOAuthConfig(credentialsPath)
	if err != nil {
		return fmt.Errorf("load OAuth configuration: %w", err)
	}

	// TODO: Create local HTTP Server and listen for the oauth redirect

	client, err := createGoogleClient(ctx, oauthConfig, tokenPath)
	if err != nil {
		return fmt.Errorf("create Google client: %w", err)
	}

	tasksService, err := createTasksService(ctx, client)
	if err != nil {
		return err
	}

	taskLists, err := GetTaskLists(ctx, tasksService)
	if err != nil {
		return fmt.Errorf("get task lists: %w", err)
	}

	taskListID, err := selectTaskList(taskLists, appConfig.TaskListID)
	if err != nil {
		return err
	}

	dueMin, dueMax, err := reportWindow(time.Now(), appConfig.WindowDays)
	if err != nil {
		return fmt.Errorf("calculate report window: %w", err)
	}

	tasks, err := GetTasks(ctx, tasksService, taskListID, GetTasksOptions{
		DueMin: &dueMin,
		DueMax: &dueMax,
	})
	if err != nil {
		return fmt.Errorf("get tasks: %w", err)
	}
	sortedTasks := SortByDueDate(tasks)

	gmailService, err := createGmailService(ctx, client)
	if err != nil {
		return fmt.Errorf("create Gmail service: %w", err)
	}

	for _, recipient := range appConfig.Recipients {
		htmlBody, err := RenderReport(recipient.Name, sortedTasks)
		if err != nil {
			return fmt.Errorf("render report for %s: %w", recipient.Address, err)
		}

		messageID, err := SendEmail(ctx, gmailService, OutgoingEmail{
			From:     appConfig.Sender,
			To:       recipient.Address,
			Subject:  appConfig.Subject,
			HTMLBody: htmlBody,
		})
		if err != nil {
			return fmt.Errorf("send report to %s: %w", recipient.Address, err)
		}
		fmt.Printf("Sent report to %s with message ID %s\n", recipient.Address, messageID)
	}

	return nil
}

func reportWindow(now time.Time, days int) (time.Time, time.Time, error) {
	if days < 1 {
		return time.Time{}, time.Time{}, fmt.Errorf("window days must be at least 1")
	}

	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, days).Add(-time.Second)
	return start, end, nil
}

func selectTaskList(taskLists []TaskList, configuredID string) (string, error) {
	if len(taskLists) == 0 {
		return "", fmt.Errorf("Google Tasks account has no task lists")
	}

	if configuredID == "" {
		return taskLists[0].ID, nil
	}

	for _, taskList := range taskLists {
		if taskList.ID == configuredID {
			return taskList.ID, nil
		}
	}

	return "", fmt.Errorf("configured task list %q was not found", configuredID)
}
