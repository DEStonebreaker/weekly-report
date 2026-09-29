//go:build darwin

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weekly-report/internal/app"
)

func TestScheduleTimePassedUsesLocalCalendarTime(t *testing.T) {
	location := time.Local
	before := time.Date(2026, 9, 29, 8, 59, 0, 0, location)
	trigger := time.Date(2026, 9, 29, 9, 0, 0, 0, location)
	after := time.Date(2026, 9, 29, 9, 1, 0, 0, location)
	for _, test := range []struct {
		name string
		now  time.Time
		want bool
	}{{"before", before, false}, {"exact", trigger, true}, {"after", after, true}, {"invalid", after, false}} {
		t.Run(test.name, func(t *testing.T) {
			value := "09:00"
			if test.name == "invalid" {
				value = "9:00"
			}
			if got := scheduleTimePassed(value, test.now); got != test.want {
				t.Fatalf("scheduleTimePassed(%q, %s) = %v, want %v", value, test.now, got, test.want)
			}
		})
	}
}

func TestRenderLaunchAgentXMLAndDailyCalendarTime(t *testing.T) {
	data := renderLaunchAgent("/Applications/Weekly & Report.app/Contents/MacOS/Weekly Report", "/Users/test/Library/Logs/Weekly Report/scheduled.log", 9, 5)
	if strings.Contains(string(data), "<key>RunAtLoad</key><true/>") || !strings.Contains(string(data), "<key>Hour</key><integer>9</integer>") || !strings.Contains(string(data), "<key>Minute</key><integer>5</integer>") {
		t.Fatalf("unexpected launch agent contents:\n%s", data)
	}
	if !strings.Contains(string(data), "Weekly &amp; Report.app") || !strings.Contains(string(data), "--scheduled-runner") {
		t.Fatalf("plist did not escape or target the packaged runner:\n%s", data)
	}
}

func TestLaunchAgentMatchesRequestedTimeAndAppPath(t *testing.T) {
	executable := "/Applications/Weekly Report.app/Contents/MacOS/Weekly Report"
	logPath := "/Users/test/Library/Logs/Weekly Report/scheduled.log"
	current := renderLaunchAgent(executable, logPath, 9, 5)
	if !launchAgentMatches(current, executable, logPath, 9, 5) {
		t.Fatal("matching installed schedule was considered inactive")
	}
	if launchAgentMatches(current, executable, logPath, 10, 5) || launchAgentMatches(current, "/Applications/Other.app/Contents/MacOS/Weekly Report", logPath, 9, 5) {
		t.Fatal("stale time or app path was considered active")
	}
}

func packagedTestExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Weekly Report.app", "Contents", "MacOS", "Weekly Report")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test executable"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLaunchAgentReadyRequiresExecutable(t *testing.T) {
	executable := packagedTestExecutable(t)
	logPath := filepath.Join(t.TempDir(), "scheduled.log")
	installed := renderLaunchAgent(executable, logPath, 9, 0)
	if !launchAgentReady(installed, executable, logPath, 9, 0) {
		t.Fatal("loaded matching schedule with executable considered inactive")
	}
	if err := os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	if launchAgentReady(installed, executable, logPath, 9, 0) {
		t.Fatal("loaded schedule with missing executable considered active")
	}
}

func TestLaunchAgentReadyRejectsNonExecutableFile(t *testing.T) {
	executable := packagedTestExecutable(t)
	if err := os.Chmod(executable, 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "scheduled.log")
	if launchAgentReady(renderLaunchAgent(executable, logPath, 9, 0), executable, logPath, 9, 0) {
		t.Fatal("loaded schedule with non-executable file considered active")
	}
}

func TestSyncLaunchAgentInstallsAndRemoves(t *testing.T) {
	home := t.TempDir()
	executablePath := packagedTestExecutable(t)
	var commands [][]string
	run := func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		if args[0] == "bootout" {
			return []byte("Boot-out failed: 3: No such process"), errors.New("launchctl exit 3")
		}
		return nil, nil
	}
	config := app.Config{ScheduleEnabled: true, ScheduleTime: "09:30"}
	active, err := syncLaunchAgent(home, config, func() (string, error) { return executablePath, nil }, run)
	if err != nil || !active {
		t.Fatalf("install returned active=%v error=%v", active, err)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
	info, err := os.Stat(plistPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("plist mode=%v error=%v", info, err)
	}
	config.ScheduleEnabled = false
	active, err = syncLaunchAgent(home, config, nil, run)
	if err != nil || active {
		t.Fatalf("remove returned active=%v error=%v", active, err)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Fatalf("disabled schedule plist still exists: %v", err)
	}
	if len(commands) != 3 || commands[0][0] != "bootout" || commands[1][0] != "bootstrap" || commands[2][0] != "bootout" {
		t.Fatalf("launchctl calls = %#v", commands)
	}
}

func TestSyncLaunchAgentRejectsDevelopmentBinary(t *testing.T) {
	called := false
	_, err := syncLaunchAgent(t.TempDir(), app.Config{ScheduleEnabled: true, ScheduleTime: "09:30"}, func() (string, error) { return "/tmp/go-build-123/Weekly Report", nil }, func(...string) ([]byte, error) {
		called = true
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "packaged app") || called {
		t.Fatalf("error=%v launchctl called=%v", err, called)
	}
}

func TestSyncLaunchAgentRejectsMissingPackagedExecutable(t *testing.T) {
	path := packagedTestExecutable(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	called := false
	_, err := syncLaunchAgent(home, app.Config{ScheduleEnabled: true, ScheduleTime: "09:30"}, func() (string, error) { return path, nil }, func(...string) ([]byte, error) {
		called = true
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "app executable is unavailable") || called {
		t.Fatalf("error=%v launchctl called=%v", err, called)
	}
	if _, statErr := os.Stat(filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")); !os.IsNotExist(statErr) {
		t.Fatalf("schedule was written for a missing executable: %v", statErr)
	}
}

func TestSyncLaunchAgentRestoresOldFileIfActivationFails(t *testing.T) {
	home := t.TempDir()
	launchAgents := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgents, 0700); err != nil {
		t.Fatal(err)
	}
	plistPath := filepath.Join(launchAgents, launchAgentLabel+".plist")
	old := []byte("previous schedule")
	if err := os.WriteFile(plistPath, old, 0600); err != nil {
		t.Fatal(err)
	}
	bootstrap := 0
	run := func(args ...string) ([]byte, error) {
		if args[0] == "bootout" {
			return nil, nil
		}
		bootstrap++
		if bootstrap == 1 {
			return []byte("invalid plist"), errors.New("exit 5")
		}
		return nil, nil
	}
	executablePath := packagedTestExecutable(t)
	_, err := syncLaunchAgent(home, app.Config{ScheduleEnabled: true, ScheduleTime: "10:00"}, func() (string, error) { return executablePath, nil }, run)
	if err == nil || !strings.Contains(err.Error(), "activate macOS background schedule") {
		t.Fatalf("install error = %v", err)
	}
	got, err := os.ReadFile(plistPath)
	if err != nil || string(got) != string(old) || bootstrap != 2 {
		t.Fatalf("restored plist=%q bootstrap calls=%d error=%v", got, bootstrap, err)
	}
}
