package main

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	valid := Config{
		Sender:     "Sender <sender@example.com>",
		Subject:    "Weekly Report",
		WindowDays: 7,
		TaskListID: "list-id",
		StartDate:  "2026-09-19",
		EndDate:    "2026-10-05",
		Recipients: []Recipient{
			{Name: "Recipient", Address: "recipient@example.com"},
		},
	}

	tests := []struct {
		name        string
		modify      func(*Config)
		wantErrText string
	}{
		{
			name:        "valid",
			modify:      func(*Config) {},
			wantErrText: "",
		},
		{
			name:        "invalid sender",
			modify:      func(config *Config) { config.Sender = "invalid" },
			wantErrText: "invalid sender",
		},
		{
			name:        "empty subject",
			modify:      func(config *Config) { config.Subject = "" },
			wantErrText: "subject is empty",
		},
		{
			name:        "invalid window",
			modify:      func(config *Config) { config.WindowDays = 0 },
			wantErrText: "window days",
		},
		{
			name:        "no recipients",
			modify:      func(config *Config) { config.Recipients = nil },
			wantErrText: "at least one recipient",
		},
		{
			name: "empty recipient name",
			modify: func(config *Config) {
				config.Recipients[0].Name = " "
			},
			wantErrText: "empty name",
		},
		{
			name: "invalid recipient address",
			modify: func(config *Config) {
				config.Recipients[0].Address = "invalid"
			},
			wantErrText: "invalid address",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			config.Recipients = append([]Recipient(nil), valid.Recipients...)
			test.modify(&config)

			err := config.Validate()
			if test.wantErrText == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("Validate() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), test.wantErrText) {
				t.Errorf("Validate() error = %q, want it to contain %q", err, test.wantErrText)
			}
		})
	}
}
