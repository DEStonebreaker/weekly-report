package main

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Sender     string      `toml:"sender"`
	Subject    string      `toml:"subject"`
	TaskListID string      `toml:"task_list_id"`
	StartDate  string      `toml:"start_date"`
	EndDate    string      `toml:"end_date"`
	WindowDays int         `toml:"window_days"`
	Recipients []Recipient `toml:"recipients"`
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

func parseCalendarDate(field, value string) (time.Time, error) {
	date, err := time.Parse("2006-01-02", value)
	if err != nil || date.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("%s must be a date in YYYY-MM-DD format", field)
	}
	return date, nil
}

type Recipient struct {
	Name    string `toml:"name"`
	Address string `toml:"address"`
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.TaskListID) == "" {
		return fmt.Errorf("task_list_id is empty; use --list-task-lists to find an ID")
	}
	start, err := parseCalendarDate("start_date", config.StartDate)
	if err != nil {
		return err
	}
	end, err := parseCalendarDate("end_date", config.EndDate)
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
