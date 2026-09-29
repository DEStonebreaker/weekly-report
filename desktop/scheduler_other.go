//go:build !darwin

package main

import (
	"fmt"

	"weekly-report/internal/app"
)

func applySystemSchedule(config app.Config) (bool, error) {
	if config.ScheduleEnabled {
		return false, fmt.Errorf("automatic delivery is currently supported on macOS only")
	}
	return false, nil
}

func systemScheduleActive(app.Config) bool { return false }
