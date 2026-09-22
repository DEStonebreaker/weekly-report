# Weekly Report

Weekly Report emails a personalized report of upcoming tasks from one Google Tasks list to each configured recipient. It uses an explicit recurring date schedule and an external invoker (such as a scheduler) can run it regularly. The app sends only the window containing the machine's current local date. Fully missed windows are skipped.

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

Run `weekly-report send` to force a report during the active window. It queries and displays tasks from today's local calendar date through that window's existing end date, both inclusive. For example, on Sep 22 in the Sep 19–25 window, the forced report covers Sep 22–25. It sends to every configured recipient even if that recipient was previously sent, failed, or uncertain, and repeating `send` sends again. This is an explicit duplicate-send action. Before `start_date` or after `end_date`, `send` exits with a no-active-window error and sends nothing. A later plain `weekly-report` still uses the original Sep 19–25 window for delivery state and will not automatically resend recipients attempted by `send`.

The application stores `state.json` and `state.lock` beside the config. It records an attempt before calling Gmail. Repeated or overlapping plain runs do not resend an attempted recipient, even when the result is failed or uncertain. Forced sends keep earlier results and attempt times in each recipient's state history. Transport and server failures are marked `UNCERTAIN` because Gmail may have accepted the message before the error. Review the printed result and Gmail Sent Mail before deciding whether to follow up manually. Gmail sends are separate requests, so a batch can partially succeed. The command prints a result per recipient and exits with an error when a send failed or is uncertain.

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
