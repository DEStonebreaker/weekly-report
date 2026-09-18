package main

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"google.golang.org/api/option"
	tasksapi "google.golang.org/api/tasks/v1"
)

type TaskList struct {
	ID    string
	Title string
}

type Task struct {
	ID          string
	Title       string
	DueDate     *time.Time
	WebViewLink string
	Notes       string
}

type GetTasksOptions struct {
	DueMax *time.Time
	DueMin *time.Time
}

func createTasksService(ctx context.Context, client *http.Client) (*tasksapi.Service, error) {
	service, err := tasksapi.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create Google Tasks service: %w", err)
	}

	return service, nil
}

func GetTasks(
	ctx context.Context,
	service *tasksapi.Service,
	taskListID string,
	opts GetTasksOptions,
) ([]Task, error) {
	call := service.Tasks.List(taskListID).ShowCompleted(false)
	if opts.DueMin != nil {
		call.DueMin(opts.DueMin.Format(time.RFC3339))
	}
	if opts.DueMax != nil {
		call.DueMax(opts.DueMax.Format(time.RFC3339))
	}

	var tasks []Task
	err := call.Pages(ctx, func(page *tasksapi.Tasks) error {
		for _, apiTask := range page.Items {
			if apiTask == nil {
				continue
			}

			task, err := taskFromAPI(apiTask)
			if err != nil {
				return err
			}
			tasks = append(tasks, task)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list Google tasks: %w", err)
	}

	return tasks, nil
}

func GetTaskLists(ctx context.Context, service *tasksapi.Service) ([]TaskList, error) {
	var taskLists []TaskList
	err := service.Tasklists.List().Pages(ctx, func(page *tasksapi.TaskLists) error {
		for _, apiTaskList := range page.Items {
			if apiTaskList == nil {
				continue
			}
			taskLists = append(taskLists, TaskList{
				ID:    apiTaskList.Id,
				Title: apiTaskList.Title,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list Google task lists: %w", err)
	}

	return taskLists, nil
}

func taskFromAPI(apiTask *tasksapi.Task) (Task, error) {
	var dueDate *time.Time
	if apiTask.Due != "" {
		parsed, err := time.Parse(time.RFC3339, apiTask.Due)
		if err != nil {
			return Task{}, fmt.Errorf("parse due date for task %q: %w", apiTask.Id, err)
		}
		dueDate = &parsed
	}

	return Task{
		ID:          apiTask.Id,
		Title:       apiTask.Title,
		DueDate:     dueDate,
		WebViewLink: apiTask.WebViewLink,
		Notes:       apiTask.Notes,
	}, nil
}

func SortByDueDate(tasks []Task) []Task {
	sorted := slices.Clone(tasks)

	slices.SortFunc(sorted, func(a, b Task) int {
		switch {
		case a.DueDate == nil && b.DueDate == nil:
			return 0
		case a.DueDate == nil:
			return 1
		case b.DueDate == nil:
			return -1
		default:
			return a.DueDate.Compare(*b.DueDate)
		}
	})

	return sorted
}
