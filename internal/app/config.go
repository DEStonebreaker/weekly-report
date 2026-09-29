package app

import (
	"bytes"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"weekly-report/internal/report"
)

type Config struct {
	Sender          string      `toml:"sender" json:"sender"`
	Subject         string      `toml:"subject" json:"subject"`
	TaskListID      string      `toml:"task_list_id" json:"taskListId"`
	StartDate       string      `toml:"start_date" json:"startDate"`
	EndDate         string      `toml:"end_date" json:"endDate"`
	WindowDays      int         `toml:"window_days" json:"windowDays"`
	ScheduleEnabled bool        `toml:"schedule_enabled" json:"scheduleEnabled"`
	ScheduleTime    string      `toml:"schedule_time" json:"scheduleTime"`
	Recipients      []Recipient `toml:"recipients" json:"recipients"`
}

func loadConfig(path string) (Config, error) {
	var config Config
	metadata, err := toml.DecodeFile(path, &config)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration %s: %w", path, err)
	}
	if keys := metadata.Undecoded(); len(keys) != 0 {
		return Config{}, fmt.Errorf("unknown configuration key %s", keys[0])
	}
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate configuration: %w", err)
	}
	return config, nil
}

// LoadSettingsDraft reads an existing form configuration without requiring it to
// be complete, so desktop onboarding can edit settings before the first save.
func loadConfigDraft(path string) (Config, error) {
	var config Config
	metadata, err := toml.DecodeFile(path, &config)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration %s: %w", path, err)
	}
	if keys := metadata.Undecoded(); len(keys) != 0 {
		return Config{}, fmt.Errorf("unknown configuration key %s", keys[0])
	}
	return config, nil
}

type Recipient struct {
	Name    string `toml:"name" json:"name"`
	Address string `toml:"address" json:"address"`
}

// saveConfig validates before touching disk and atomically replaces the TOML file.
func saveConfig(path string, config Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}
	var data bytes.Buffer
	if err := toml.NewEncoder(&data).Encode(config); err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err := temp.Write(data.Bytes()); err != nil {
		return fmt.Errorf("write configuration: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync configuration: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close configuration: %w", err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	return nil
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.TaskListID) == "" {
		return fmt.Errorf("task_list_id is empty; use --list-task-lists to find an ID")
	}
	start, err := report.ParseCalendarDate("start_date", config.StartDate)
	if err != nil {
		return err
	}
	end, err := report.ParseCalendarDate("end_date", config.EndDate)
	if err != nil {
		return err
	}
	if end.Before(start) {
		return fmt.Errorf("end_date is before start_date")
	}
	if _, err := mail.ParseAddress(config.Sender); err != nil {
		return fmt.Errorf("invalid sender: %w", err)
	}
	if config.Subject == "" {
		return fmt.Errorf("subject is empty")
	}
	if strings.ContainsAny(config.Subject, "\r\n") {
		return fmt.Errorf("subject contains invalid characters")
	}
	if config.WindowDays < 1 {
		return fmt.Errorf("window days must be at least 1")
	}
	if config.ScheduleEnabled && config.ScheduleTime == "" {
		return fmt.Errorf("schedule time is required when automatic delivery is enabled")
	}
	if config.ScheduleTime != "" {
		parsed, err := time.Parse("15:04", config.ScheduleTime)
		if err != nil || parsed.Format("15:04") != config.ScheduleTime {
			return fmt.Errorf("schedule time must use 24-hour HH:MM format")
		}
	}
	if len(config.Recipients) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	for index, recipient := range config.Recipients {
		if strings.TrimSpace(recipient.Name) == "" {
			return fmt.Errorf("recipient %d has an empty name", index+1)
		}
		address, err := mail.ParseAddress(recipient.Address)
		if err != nil {
			return fmt.Errorf("recipient %d has an invalid address: %w", index+1, err)
		}
		if address.Address != recipient.Address {
			return fmt.Errorf("recipient %d address must be a plain email address", index+1)
		}
		for previous := 0; previous < index; previous++ {
			if strings.EqualFold(config.Recipients[previous].Address, recipient.Address) {
				return fmt.Errorf("recipient %d duplicates address %s", index+1, recipient.Address)
			}
		}
	}

	return nil
}
