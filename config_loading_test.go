package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRejectsUnknownAndInvalidFields(t *testing.T) {
	valid := `sender = "Sender <sender@example.com>"
subject = "Report"
task_list_id = "list-id"
start_date = "2026-09-19"
end_date = "2026-10-05"
window_days = 7
[[recipients]]
name = "Alice"
address = "alice@example.com"
[[recipients]]
name = "Bob"
address = "bob@example.com"
`
	cases := []struct{ name, content, want string }{
		{"valid", valid, ""},
		{"unknown", valid + "\nunknown_field = 1\n", "unknown configuration key"},
		{"missing id", strings.Replace(valid, "task_list_id = \"list-id\"\n", "", 1), "task_list_id"},
		{"bad date", strings.Replace(valid, "2026-09-19", "2026-02-30", 1), "start_date"},
		{"reversed", strings.Replace(valid, "2026-10-05", "2026-09-18", 1), "end_date"},
		{"duplicate", strings.Replace(valid, "bob@example.com", "alice@example.com", 1), "duplicates"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			config, err := loadConfig(path)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				if len(config.Recipients) != 2 {
					t.Errorf("recipients=%d", len(config.Recipients))
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error=%v, want %q", err, tc.want)
			}
		})
	}
}
