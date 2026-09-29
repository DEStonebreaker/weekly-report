//go:build darwin

package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"weekly-report/internal/app"
)

const launchAgentLabel = "com.weeklyreport.desktop.scheduled-report"

func applySystemSchedule(config app.Config) (bool, error) {
	if config.ScheduleEnabled && systemScheduleActive(config) {
		return true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("find home directory for macOS schedule: %w", err)
	}
	return syncLaunchAgent(home, config, os.Executable, launchctl)
}

func systemScheduleActive(config app.Config) bool {
	if !config.ScheduleEnabled || launchctlActive() != nil {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	executablePath, err := os.Executable()
	if err != nil {
		return false
	}
	hour, minute, err := scheduleHourMinute(config.ScheduleTime)
	if err != nil {
		return false
	}
	logPath := filepath.Join(home, "Library", "Logs", "Weekly Report", "scheduled.log")
	path := filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
	actual, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return launchAgentMatches(actual, executablePath, logPath, hour, minute)
}

func launchAgentMatches(actual []byte, executable, logPath string, hour, minute int) bool {
	return bytes.Equal(actual, renderLaunchAgent(executable, logPath, hour, minute))
}

func syncLaunchAgent(home string, config app.Config, executable func() (string, error), run func(...string) ([]byte, error)) (bool, error) {
	launchAgents := filepath.Join(home, "Library", "LaunchAgents")
	plistPath := filepath.Join(launchAgents, launchAgentLabel+".plist")
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchAgentLabel)
	if !config.ScheduleEnabled {
		if err := bootoutLaunchAgent(target, run); err != nil {
			return false, fmt.Errorf("stop macOS background schedule: %w", err)
		}
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("remove macOS schedule file: %w", err)
		}
		return false, nil
	}
	hour, minute, err := scheduleHourMinute(config.ScheduleTime)
	if err != nil {
		return false, err
	}
	executablePath, err := executable()
	if err != nil {
		return false, fmt.Errorf("locate Weekly Report app executable: %w", err)
	}
	if !strings.Contains(executablePath, ".app/Contents/MacOS/") {
		return false, fmt.Errorf("automatic delivery can be enabled in the packaged app; build and open Weekly Report.app first")
	}
	logDir := filepath.Join(home, "Library", "Logs", "Weekly Report")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return false, fmt.Errorf("create schedule log directory: %w", err)
	}
	if err := os.MkdirAll(launchAgents, 0700); err != nil {
		return false, fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	data := renderLaunchAgent(executablePath, filepath.Join(logDir, "scheduled.log"), hour, minute)
	previous, readErr := os.ReadFile(plistPath)
	previousExists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, fmt.Errorf("read previous macOS schedule: %w", readErr)
	}
	if err := writeAtomic(plistPath, data, 0600); err != nil {
		return false, fmt.Errorf("write macOS schedule: %w", err)
	}
	if err := bootoutLaunchAgent(target, run); err != nil {
		_ = restoreLaunchAgent(plistPath, previous, previousExists, target, run)
		return false, fmt.Errorf("replace existing macOS schedule: %w", err)
	}
	if _, err := run("bootstrap", targetDomain(), plistPath); err != nil {
		_ = restoreLaunchAgent(plistPath, previous, previousExists, target, run)
		return false, fmt.Errorf("activate macOS background schedule: %s", strings.TrimSpace(stringError(err)))
	}
	return true, nil
}

func scheduleHourMinute(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return 0, 0, fmt.Errorf("schedule time must use 24-hour HH:MM format")
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func renderLaunchAgent(executable, logPath string, hour, minute int) []byte {
	escape := func(value string) string {
		var output bytes.Buffer
		_ = xml.EscapeText(&output, []byte(value))
		return output.String()
	}
	contents := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>--scheduled-runner</string></array>
<key>StartCalendarInterval</key><dict><key>Hour</key><integer>%s</integer><key>Minute</key><integer>%s</integer></dict>
<key>RunAtLoad</key><false/>
<key>ProcessType</key><string>Background</string>
<key>StandardOutPath</key><string>%s</string>
<key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, escape(launchAgentLabel), escape(executable), strconv.Itoa(hour), strconv.Itoa(minute), escape(logPath), escape(logPath))
	return []byte(contents)
}

func bootoutLaunchAgent(target string, run func(...string) ([]byte, error)) error {
	output, err := run("bootout", target)
	if err == nil || strings.Contains(strings.ToLower(string(output)), "could not find specified service") || strings.Contains(strings.ToLower(string(output)), "service not found") || strings.Contains(strings.ToLower(string(output)), "no such process") {
		return nil
	}
	return fmt.Errorf("%s", strings.TrimSpace(string(output)))
}

func restoreLaunchAgent(path string, previous []byte, existed bool, target string, run func(...string) ([]byte, error)) error {
	if existed {
		if err := writeAtomic(path, previous, 0600); err != nil {
			return err
		}
		_, err := run("bootstrap", targetDomain(), path)
		return err
	}
	_ = os.Remove(path)
	return nil
}

func launchctl(args ...string) ([]byte, error) {
	command := exec.Command("/bin/launchctl", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return output, nil
}

func launchctlActive() error {
	_, err := launchctl("print", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchAgentLabel))
	return err
}

func targetDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".schedule-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func stringError(err error) string {
	if err == nil {
		return "unknown launchctl error"
	}
	return err.Error()
}
