# Weekly Report

Weekly Report emails a personalized report of upcoming tasks from one Google Tasks list to each configured recipient. It uses an explicit recurring date schedule and an external invoker (such as a scheduler) can run it regularly. A scheduled run sends only the window containing the machine's current local date. Fully missed windows are skipped.

## Setup

Place Google OAuth `credentials.json` in the `weekly-tasks` directory under your OS user configuration directory. On macOS this is typically `~/Library/Application Support/weekly-tasks/`. Run:

```sh
weekly-report --init
weekly-report --list-task-lists
```

`--init` opens an interactive Google authorization flow and saves `token.json` in the same directory. It works without a report config. Paste the full browser redirect URL when prompted. `--list-task-lists` prints each list's title and Google ID using the saved token. Neither this command nor a normal report run starts interactive authorization. If the token is missing or revoked, run `--init` again.

Create `config.toml` in that directory:

```toml
sender = "Sender <sender@example.com>"
subject = "Weekly Report"
task_list_id = "google-issued-list-id"
start_date = "2026-09-19"
end_date = "2026-10-05"
window_days = 7

[[recipients]]
name = "Alice"
address = "alice@example.com"

[[recipients]]
name = "Bob"
address = "bob@example.com"
```

The dates are inclusive calendar dates. The example sends reports for Sep 19–25, Sep 26–Oct 2, and Oct 3–5. Each recipient gets the same task list and date range, with a personal greeting. Invoke `weekly-report` as often as needed; it attempts each recipient only once per window. There is no built-in scheduler or LaunchAgent setup.

## Manual report ranges

`weekly-report send` sends to every configured recipient for seven calendar dates starting today. On September 22, this covers September 22–28, inclusive. Manual ranges are independent of the configured schedule and can fall before `start_date` or after `end_date`.

| Command | Included dates |
| --- | --- |
| `weekly-report send` | Today through today + 6 days |
| `weekly-report send --days 14` | Today through today + 13 days |
| `weekly-report send --this-week` | Monday–Sunday of the current week |
| `weekly-report send --next-week` | Monday–Sunday of the following week |
| `weekly-report send --from 2026-09-25 --to 2026-10-02` | September 25–October 2, inclusive |

Choose one range option. `--days` must be positive; `--from` and `--to` must be supplied together in YYYY-MM-DD format, with the end on or after the start. Today is the machine's local calendar date. Explicit dates and calendar weeks include incomplete tasks within their range even when those dates are in the past. Overdue tasks outside the selected range are not added. Completed tasks and tasks without a date are excluded.

Every manual invocation sends again. Manual sends neither read nor write scheduled delivery state, so they do not suppress a later scheduled report, and an earlier scheduled report does not suppress them. Manual reports still use the list, sender, subject, and recipients from the same validated config.

## Scheduled delivery state

Scheduled runs store `state.json` and `state.lock` beside the config and record an attempt before calling Gmail. Repeated or overlapping scheduled runs do not resend an attempted recipient, even when the result is failed or uncertain. Their configured fixed windows remain unchanged.

Transport and server failures are marked `UNCERTAIN` because Gmail may have accepted the message before the error. Review the printed result and Gmail Sent Mail before deciding whether to follow up manually. Gmail sends are separate requests, so a batch can partially succeed. Both modes print a result per recipient and exit with an error when a send failed or is uncertain.

Run `weekly-report --help` for the current flags.

## Development

The project uses one Go module with three internal packages:

- `internal/app`: application modes, configuration, delivery policy, and persistent delivery state.
- `internal/report`: calendar windows, task data and sorting, and HTML rendering. The embedded report template lives here beside the renderer.
- `internal/google`: OAuth, Google Tasks, Gmail, and provider-specific delivery error classification.

`main.go` parses CLI arguments and calls the application. The application coordinates the report and Google packages; the report package has no Google SDK dependency.

Tests live beside the code they exercise. CLI parsing tests remain at the root; application workflow tests use fake HTTP responses and temporary state directories without contacting Google.

```sh
go test ./...
go test -race ./...
go test ./internal/report
go build .
```
