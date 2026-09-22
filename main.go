package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alecthomas/kong"
	"weekly-report/internal/app"
)

type CLI struct {
	Init          bool     `help:"Authorize Google access and save a token." xor:"mode"`
	ListTaskLists bool     `help:"Print task list titles and IDs." xor:"mode"`
	Run           struct{} `cmd:"" default:"1" hidden:""`
	Send          struct{} `cmd:"" help:"Force a report for today through the active window's end."`
	forceSend     bool
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cli, err := parseCLI(os.Args[1:])
	if err != nil {
		return err
	}
	return app.Run(ctx, app.Options{Init: cli.Init, ListTaskLists: cli.ListTaskLists, ForceSend: cli.forceSend}, time.Now(), os.Stdout)
}

func parseCLI(args []string) (CLI, error) {
	var cli CLI
	parser, err := kong.New(&cli, kong.Name("weekly-report"), kong.Description("Run without a command for the scheduled report, or use send to force a report. Run --init first, then --list-task-lists to find an ID. Configuration is config.toml in the weekly-tasks user config directory."))
	if err != nil {
		return CLI{}, err
	}
	parsed, err := parser.Parse(args)
	if err != nil {
		return CLI{}, err
	}
	if err := parsed.Validate(); err != nil {
		return CLI{}, err
	}
	cli.forceSend = parsed.Command() == "send"
	if cli.forceSend && (cli.Init || cli.ListTaskLists) {
		return CLI{}, fmt.Errorf("send cannot be combined with --init or --list-task-lists")
	}
	return cli, nil
}
