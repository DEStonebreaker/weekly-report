package app

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"golang.org/x/oauth2"

	"weekly-report/internal/google"
	"weekly-report/internal/report"
)

// Options selects the application operation independently of CLI parsing.
type Options struct {
	Init          bool
	ListTaskLists bool
	ManualSend    bool
	Range         report.RangeOptions
}

func Run(ctx context.Context, cli Options, now time.Time, output io.Writer) error {
	service, err := NewDefaultService()
	if err != nil {
		return err
	}
	return runService(ctx, service, cli, now, output, google.AuthorizeWithGoogle)
}

func runModeInDir(ctx context.Context, cli Options, now time.Time, output io.Writer, appDir string, authorize func(context.Context, *oauth2.Config) (*oauth2.Token, error)) error {
	return runService(ctx, NewService(appDir), cli, now, output, authorize)
}

// runService is the CLI presentation adapter; application operations return data.
func runService(ctx context.Context, service *Service, cli Options, now time.Time, output io.Writer, authorize func(context.Context, *oauth2.Config) (*oauth2.Token, error)) error {
	if cli.Init {
		if err := service.InitializeGoogle(ctx, authorize); err != nil {
			return err
		}
		fmt.Fprintln(output, "Google authorization saved.")
		return nil
	}
	if cli.ListTaskLists {
		lists, err := service.ListTaskLists(ctx)
		if err != nil {
			return err
		}
		writer := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
		fmt.Fprintln(writer, "TITLE\tID")
		for _, list := range lists {
			fmt.Fprintf(writer, "%s\t%s\n", list.Title, list.ID)
		}
		return writer.Flush()
	}
	var results []DeliveryResult
	var err error
	if cli.ManualSend {
		results, err = service.SendManualReport(ctx, cli.Range, now)
	} else {
		results, err = service.SendScheduledReport(ctx, now)
	}
	if results != nil {
		printSendResults(output, results)
	}
	return err
}

func printSendResults(output io.Writer, results []DeliveryResult) {
	sent, failed, uncertain, skipped := 0, 0, 0, 0
	for _, result := range results {
		switch result.Status {
		case "SENT":
			sent++
			fmt.Fprintf(output, "SENT\t%s\tmessage ID %s\n", result.Recipient.Address, result.MessageID)
		case "FAILED":
			failed++
			fmt.Fprintf(output, "FAILED\t%s\t%v\n", result.Recipient.Address, result.Error)
		case "UNCERTAIN":
			uncertain++
			fmt.Fprintf(output, "UNCERTAIN\t%s\t%v\n", result.Recipient.Address, result.Error)
		default:
			skipped++
			if result.Error != "" {
				fmt.Fprintf(output, "SKIPPED\t%s\t%v\n", result.Recipient.Address, result.Error)
			} else {
				fmt.Fprintf(output, "SKIPPED\t%s\tpreviously attempted\n", result.Recipient.Address)
			}
		}
	}
	fmt.Fprintf(output, "%d sent, %d failed, %d uncertain, %d skipped\n", sent, failed, uncertain, skipped)
}
