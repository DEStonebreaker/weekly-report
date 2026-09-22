package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"golang.org/x/oauth2"

	"weekly-report/internal/google"
	"weekly-report/internal/report"
)

// Options selects the application operation independently of CLI parsing.
type Options struct {
	Init          bool
	ListTaskLists bool
	ManualSend    bool
	Range         report.RangeOptions
}

func Run(ctx context.Context, cli Options, now time.Time, output io.Writer) error {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("find user configuration directory: %w", err)
	}
	appDir := filepath.Join(configRoot, "weekly-tasks")
	return runModeInDir(ctx, cli, now, output, appDir, google.AuthorizeWithGoogle)
}

func runModeInDir(ctx context.Context, cli Options, now time.Time, output io.Writer, appDir string, authorize func(context.Context, *oauth2.Config) (*oauth2.Token, error)) error {
	var err error
	tokenPath := filepath.Join(appDir, "token.json")
	credentialsPath := filepath.Join(appDir, "credentials.json")
	if cli.Init {
		oauthConfig, err := google.LoadOAuthConfig(credentialsPath)
		if err != nil {
			return err
		}
		if err := google.InitializeToken(ctx, oauthConfig, tokenPath, authorize); err != nil {
			return err
		}
		fmt.Fprintln(output, "Google authorization saved.")
		return nil
	}
	var config Config
	var window report.ReportWindow
	var state *DeliveryState
	if !cli.ListTaskLists {
		config, err = loadConfig(filepath.Join(appDir, "config.toml"))
		if err != nil {
			return err
		}
		if cli.ManualSend {
			window, err = cli.Range.Resolve(now)
			if err != nil {
				return err
			}
		} else {
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
	}
	oauthConfig, err := google.LoadOAuthConfig(credentialsPath)
	if err != nil {
		return err
	}
	client, err := google.SavedGoogleClient(ctx, oauthConfig, tokenPath)
	if err != nil {
		return err
	}
	tasksService, err := google.CreateTasksService(ctx, client)
	if err != nil {
		return err
	}
	taskLists, err := google.GetTaskLists(ctx, tasksService)
	if err != nil {
		return fmt.Errorf("get task lists: %w", google.WithAuthHint(err))
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
