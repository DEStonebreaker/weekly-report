package main

import (
	"fmt"
	"net/mail"
	"strings"
)

type Config struct {
	Sender     string      `toml:"sender"`
	Subject    string      `toml:"subject"`
	TaskListID string      `toml:"task_list_id"`
	WindowDays int         `toml:"window_days"`
	Recipients []Recipient `toml:"recipients"`
}

type Recipient struct {
	Name    string `toml:"name"`
	Address string `toml:"address"`
}

func (config Config) Validate() error {
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
		if _, err := mail.ParseAddress(recipient.Address); err != nil {
			return fmt.Errorf("recipient %d has an invalid address: %w", index+1, err)
		}
	}

	return nil
}
