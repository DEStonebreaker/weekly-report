package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		panic(fmt.Errorf("find user configuration directory: %w", err))
	}

	appDir := filepath.Join(configRoot, "weekly-tasks")
	credentialsPath := filepath.Join(appDir, "credentials.json")
	tokenPath := filepath.Join(appDir, "token.json")

	config, err := createConfig(credentialsPath)
	if err != nil {
		panic(fmt.Errorf("create config: %w", err))
	}

	// TODO: Create local HTTP Server and listen for the oauth redirect

	ctx := context.Background()
	client, err := createGoogleClient(ctx, config, tokenPath)
	if err != nil {
		panic(fmt.Errorf("create Google client: %w", err))
	}

	tasklists, err := GetTasklists(client)
	if err != nil {
		panic(fmt.Errorf("get tasklists: %w", err))
	}
	// tasklists[].ID

	var DueMax = time.Now().AddDate(0, 0, 7)

	capture, err := GetTasks(client, tasklists[0].ID,
		GetTasksOptions{DueMax: &DueMax})
	if err != nil {
		panic(fmt.Errorf("get tasks: %w", err))
	}

	for _, task := range capture {
		fmt.Printf("- %s, DUE@ %v-%v-%v\n", task.Title, task.DueDate.Day(), task.DueDate.Month(), task.DueDate.Year())
	}
}
