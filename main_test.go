package main

import "testing"

func TestSendCommandParsing(t *testing.T) {
	for _, tc := range []struct {
		args              []string
		force, init, list bool
		wantErr           bool
	}{
		{args: nil},
		{args: []string{"send"}, force: true},
		{args: []string{"--init"}, init: true},
		{args: []string{"--list-task-lists"}, list: true},
		{args: []string{"send", "--init"}, wantErr: true},
		{args: []string{"send", "--list-task-lists"}, wantErr: true},
	} {
		cli, err := parseCLI(tc.args)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%v: wanted error", tc.args)
			}
			continue
		}
		if err != nil {
			t.Errorf("%v: %v", tc.args, err)
			continue
		}
		if cli.manualSend != tc.force || cli.Init != tc.init || cli.ListTaskLists != tc.list {
			t.Errorf("%v: %+v", tc.args, cli)
		}
	}
}

func TestSendRangeParsing(t *testing.T) {
	for _, args := range [][]string{
		{"send", "--days", "14"},
		{"send", "--this-week"},
		{"send", "--next-week"},
		{"send", "--from", "2026-08-01", "--to", "2026-08-03"},
	} {
		cli, err := parseCLI(args)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if !cli.manualSend {
			t.Errorf("%v: not a manual send", args)
		}
	}
	for _, args := range [][]string{
		{"send", "--days", "0"},
		{"send", "--days", "-1"},
		{"send", "--days", "7", "--this-week"},
		{"send", "--this-week", "--next-week"},
		{"send", "--from", "2026-09-22"},
		{"send", "--to", "2026-09-22"},
		{"send", "--from", "2026-09-25", "--to", "2026-09-22"},
		{"send", "--from", "2026-02-30", "--to", "2026-03-01"},
		{"send", "--from", "2026-09-22", "--to", "2026-09-25", "--days", "7"},
		{"--days", "7"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Errorf("%v: expected error", args)
		}
	}
}
