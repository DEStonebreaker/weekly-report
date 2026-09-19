package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/alecthomas/kong"
	"golang.org/x/oauth2"
)

type CLI struct {
	Init          bool `help:"Authorize Google access and save a token." xor:"mode"`
	ListTaskLists bool `help:"Print task list titles and IDs." xor:"mode"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var cli CLI
	parser, err := kong.New(&cli, kong.Name("weekly-report"), kong.Description("Send a recurring Google Tasks report. Run --init first, then --list-task-lists to find an ID. Configuration is config.toml in the weekly-tasks user config directory."))
	if err != nil {
		return err
	}
	parsed, err := parser.Parse(os.Args[1:])
	if err != nil {
		return err
	}
	if err := parsed.Validate(); err != nil {
		return err
	}
	return runMode(ctx, cli, time.Now(), os.Stdout)
}

func runMode(ctx context.Context, cli CLI, now time.Time, output io.Writer) error {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("find user configuration directory: %w", err)
	}
	appDir := filepath.Join(configRoot, "weekly-tasks")
	return runModeInDir(ctx, cli, now, output, appDir, authorizeWithGoogle)
}

func runModeInDir(ctx context.Context, cli CLI, now time.Time, output io.Writer, appDir string, authorize func(context.Context, *oauth2.Config) (*oauth2.Token, error)) error {
	var err error
	tokenPath := filepath.Join(appDir, "token.json")
	credentialsPath := filepath.Join(appDir, "credentials.json")
	if cli.Init {
		oauthConfig, err := loadOAuthConfig(credentialsPath)
		if err != nil {
			return err
		}
		if err := initializeToken(ctx, oauthConfig, tokenPath, authorize); err != nil {
			return err
		}
		fmt.Fprintln(output, "Google authorization saved.")
		return nil
	}
	var config Config
	var window ReportWindow
	var state *DeliveryState
	if !cli.ListTaskLists {
		config, err = loadConfig(filepath.Join(appDir, "config.toml"))
		if err != nil {
			return err
		}
		window, err = activeWindow(config, now)
		if err != nil {
			return err
		}
		if window.Start.IsZero() {
			return nil
		}
		state, err = openDeliveryState(appDir)
		if err != nil {
			return err
		}
		defer state.Close()
		if results, complete := existingResults(config, window, state); complete {
			printSendResults(output, results)
			return resultError(results)
		}
	}
	oauthConfig, err := loadOAuthConfig(credentialsPath)
	if err != nil {
		return err
	}
	client, err := savedGoogleClient(ctx, oauthConfig, tokenPath)
	if err != nil {
		return err
	}
	tasksService, err := createTasksService(ctx, client)
	if err != nil {
		return err
	}
	taskLists, err := GetTaskLists(ctx, tasksService)
	if err != nil {
		return fmt.Errorf("get task lists: %w", withAuthHint(err))
	}
	if cli.ListTaskLists {
		writer := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
		fmt.Fprintln(writer, "TITLE\tID")
		for _, list := range taskLists {
			fmt.Fprintf(writer, "%s\t%s\n", list.Title, list.ID)
		}
		return writer.Flush()
	}
	return sendReport(ctx, config, window, state, taskLists, tasksService, client, output)
}

type ReportWindow struct {
	Start time.Time // UTC calendar date
	End   time.Time // UTC calendar date, inclusive
}

func activeWindow(config Config, now time.Time) (ReportWindow, error) {
	start, err := parseCalendarDate("start_date", config.StartDate)
	if err != nil {
		return ReportWindow{}, err
	}
	end, err := parseCalendarDate("end_date", config.EndDate)
	if err != nil {
		return ReportWindow{}, err
	}
	if config.WindowDays < 1 {
		return ReportWindow{}, fmt.Errorf("window_days must be at least 1")
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if today.Before(start) || today.After(end) {
		return ReportWindow{}, nil
	}
	days := int(today.Sub(start).Hours() / 24)
	windowStart := start.AddDate(0, 0, days/config.WindowDays*config.WindowDays)
	windowEnd := windowStart.AddDate(0, 0, config.WindowDays-1)
	if windowEnd.After(end) {
		windowEnd = end
	}
	return ReportWindow{windowStart, windowEnd}, nil
}

func selectTaskList(taskLists []TaskList, configuredID string) (string, error) {
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
