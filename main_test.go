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
		if cli.forceSend != tc.force || cli.Init != tc.init || cli.ListTaskLists != tc.list {
			t.Errorf("%v: %+v", tc.args, cli)
		}
	}
}
