package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const tasksBaseEndpoint string = "https://tasks.googleapis.com"

/* TasksAPI structs, source:
 * https://www.googleapis.com/discovery/v1/apis/tasks/v1/rest
 */

type TaskList struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Updated time.Time `json:"updated"`
}

type TaskListsResponse struct {
	Items         []TaskList `json:"items"`
	NextPageToken string     `json:"nextPageToken"`
}

type TaskStatus string

const (
	TaskNeedsAction TaskStatus = "needsAction"
	TaskCompleted   TaskStatus = "completed"
)

type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	DueDate     *time.Time `json:"due"`
	Updated     time.Time  `json:"updated"`
	Status      TaskStatus `json:"status"`
	Deleted     bool       `json:"deleted"`
	Hidden      bool       `json:"hidden"`
	WebViewLink string     `json:"webViewLink"`
	Notes       string     `json:"notes"`
}

type TaskListResponse struct {
	Items         []Task `json:"items"`
	NextPageToken string `json:"nextPageToken"`
}

type GetTasksOptions struct {
	DueMax    *time.Time
	PageToken string
}

func GetTasks(
	client *http.Client,
	taskListID string,
	opts GetTasksOptions,
) ([]Task, error) {

	var tasks []Task
	var pageToken string

	for {
		n_url, err := url.Parse(strings.Join(
			[]string{
				tasksBaseEndpoint,
				"tasks",
				"v1",
				"lists",
				taskListID,
				"tasks",
			},
			"/",
		))
		if err != nil {
			return nil, err
		}

		query := n_url.Query()

		if opts.DueMax != nil {
			query.Set("dueMax", opts.DueMax.Format(time.RFC3339))
		}

		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}

		n_url.RawQuery = query.Encode()

		resp, err := client.Get(n_url.String())
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf(
				"Google Tasks API returned status %s",
				resp.Status,
			)
		}

		var result TaskListResponse

		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if err != nil {
			return nil, err
		}

		tasks = append(tasks, result.Items...)

		if result.NextPageToken == "" {
			break
		}

		pageToken = result.NextPageToken
	}

	return tasks, nil
}

func GetTasklists(client *http.Client) ([]TaskList, error) {
	var tasklists []TaskList
	var pageToken string

	for {
		n_url, err := url.Parse(strings.Join(
			[]string{
				tasksBaseEndpoint,
				"tasks",
				"v1",
				"users",
				"@me",
				"lists",
			},
			"/"))
		if err != nil {
			return nil, err
		}
		query := n_url.Query()
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}

		n_url.RawQuery = query.Encode()
		resp, err := client.Get(n_url.String())
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf(
				"Google Tasks API returned status %s",
				resp.Status,
			)
		}

		var result TaskListsResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if err != nil {
			return nil, err
		}
		tasklists = append(tasklists, result.Items...)
		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}
	return tasklists, nil
}
