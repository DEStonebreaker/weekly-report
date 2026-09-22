package app

import (
	"strings"
	"testing"

	"weekly-report/internal/google"
)

func TestSelectTaskList(t *testing.T) {
	lists := []google.TaskList{{ID: "first", Title: "First"}, {ID: "configured", Title: "Configured"}}
	if got, err := selectTaskList(lists, "configured"); err != nil || got != "configured" {
		t.Errorf("got %s, %v", got, err)
	}
	for _, id := range []string{"", "missing"} {
		if _, err := selectTaskList(lists, id); err == nil || !strings.Contains(err.Error(), "--list-task-lists") {
			t.Errorf("id %q: %v", id, err)
		}
	}
}
