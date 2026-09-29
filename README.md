# Weekly Report

Weekly Report emails a personalized report of upcoming tasks from one Google Tasks list to each configured recipient. Scheduled runs send only the active date window. The CLI can be invoked by an external scheduler; the macOS desktop app can manage its own daily LaunchAgent.

## Setup

Place Google OAuth **Desktop app** `credentials.json` in the `weekly-tasks` directory under your OS user configuration directory. On macOS this is typically `~/Library/Application Support/weekly-tasks/`. Run:

```sh
weekly-report --init
weekly-report --list-task-lists
```

`--init` opens an interactive Google authorization flow and saves `token.json` in the same directory. It works without a report config. Open the printed URL in a browser on the same machine and approve access within five minutes. A temporary loopback HTTP server receives the callback automatically, so there is no redirect URL to paste. The callback uses an available local port; no redirect URL needs to be added to `credentials.json` or `config.toml`. `--list-task-lists` prints each list's title and Google ID using the saved token. Neither this command nor a normal report run starts interactive authorization. If the token is missing or revoked, run `--init` again.

Create `config.toml` in that directory:

```toml
sender = "Sender <sender@example.com>"
subject = "Weekly Report"
task_list_id = "google-issued-list-id"
start_date = "2026-09-19"
end_date = "2026-10-05"
window_days = 7
# Desktop only: enable automatic daily delivery at this local time.
schedule_enabled = false
schedule_time = "09:00"

[[recipients]]
name = "Alice"
address = "alice@example.com"

[[recipients]]
name = "Bob"
address = "bob@example.com"
```

The dates are inclusive calendar dates. The example sends reports for Sep 19–25, Sep 26–Oct 2, and Oct 3–5. Each recipient gets the same task list and date range, with a personal greeting. Invoke `weekly-report` as often as needed; it attempts each recipient only once per window. The `schedule_enabled` and `schedule_time` fields are used by the macOS desktop app; CLI scheduled runs can still be invoked externally regardless of that toggle.

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

In the macOS desktop app, enable **Automatic delivery**, choose a local time, and
save settings. Weekly Report installs a per-user LaunchAgent that runs a headless
mode from the app bundle once daily; the GUI can close while the user remains logged
in. Keep the app bundle at its installed path, or reopen it from its new location and
save settings so the LaunchAgent path is updated. Turn off automatic delivery in the
app to unload and remove its LaunchAgent.

If the Mac sleeps through the scheduled time, launchd runs the job when it wakes. If
the Mac is powered off, the next run is at the next daily time. Opening Weekly Report
after the selected time also catches up if the configured report window is still
active. Expired windows are skipped. An overlapping run is rejected by the process
lock. Failed or uncertain recipient attempts are never retried automatically. The
app shows the latest scheduled results; the headless runner log is at
`~/Library/Logs/Weekly Report/scheduled.log`. Scheduling requires the packaged macOS
app and a logged-in user session; `wails dev` cannot install a persistent job.

Transport and server failures are marked `UNCERTAIN` because Gmail may have accepted the message before the error. Review the printed result and Gmail Sent Mail before deciding whether to follow up manually. Gmail sends are separate requests, so a batch can partially succeed. Both modes print a result per recipient and exit with an error when a send failed or is uncertain.

Run `weekly-report --help` for the current flags.

## Development

### Desktop app (milestones 1–5)

The Wails desktop entry point lives in `desktop/` and shares the root Go module.
The desktop provides a React/TypeScript onboarding screen, Google sign-in in the
system browser, task-list selection, a settings editor, and a review-before-send
manual report workflow, and optional daily background delivery through macOS
launchd.

The initial target is macOS on Apple Silicon. Install the Go version required by
`go.mod`, Node.js 22.12+ (or a newer supported release), npm, and Xcode command-line
tools. Install the matching Wails CLI:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
```

Start development from the desktop directory:

```sh
cd desktop
wails dev
```

Wails installs the locked frontend dependencies, generates TypeScript bindings,
and launches the native window with Vite hot reload. The setup screen reports
whether Google is connected and calls Go for sign-in, task lists, settings,
preparation, and delivery. Stop with Ctrl+C in the terminal. Running only Vite in a
browser does not provide the native Go bridge.

Build and launch the macOS application:

```sh
cd desktop # from the repository root
wails build
open "build/bin/weekly-report-desktop.app"
```

The app bundles its frontend assets and does not need a Vite server, Node, or Go
installed to run. This is a local development bundle, not a signed and notarized
public release. Other operating systems and Intel Macs have not been validated.

For a smoke test, check the connection in both development and packaged builds,
resize the window, and reach the connection button using the keyboard. Quit the
development process before testing the packaged app so the two are distinguishable.
Generated Go bindings are checked in; Wails refreshes them when the Go API changes.
Frontend dependencies, runtime bindings, and compiled assets are ignored. The
tracked `dist/.gitkeep` permits Go package discovery before the first frontend build.

The settings editor covers sender, subject, task list, schedule dates, window
length, and recipients. Existing settings load into the form; saving validates the
complete configuration and preserves the previous file if validation fails. Google
tokens stay in the OS configuration directory. OAuth credentials are not bundled
yet: place your Google Desktop app `credentials.json` at the path shown in the app
before connecting. The browser callback returns automatically to Weekly Report, and
a revoked connection can be renewed with **Reconnect Google**.

The **Preview and send** section supports the next 7 or 14 days, this week, next
week, and an inclusive custom date range. Preparing reads Tasks and builds every
recipient's personalized message without sending. Each message is shown in a
sandboxed preview. Editing report settings or dates clears the prepared preview;
save changed settings and prepare again. Sending uses the exact reviewed snapshot
once, reports live progress and an outcome for each recipient, and never retries an
uncertain or failed delivery automatically. A new send requires a new preparation.

The CLI continues to build from the repository root using `go build .`.

### Go packages

The desktop roadmap is in [aidocs/desktop-milestones.md](aidocs/desktop-milestones.md).
`app.Service` is shared by CLI operations and the desktop:

- `LoadSettings` / `SaveSettings` validate and atomically persist TOML settings.
- `PrepareReport` reads tasks and returns an opaque, in-memory report snapshot.
  Its `Preview()` method returns detached HTML and recipient data without sending
  email or touching scheduled delivery state.
- `SendPreparedReport` sends the prepared content once and returns ordered,
  JSON-compatible `DeliveryResult` values. It returns results alongside an error
  for partial failure or cancellation; callers must inspect both.
- `SendPreparedReportWithProgress` reports concurrent recipient completion events;
  callers must make the callback concurrency-safe.
- `SendManualReport` and `SendScheduledReport` preserve the CLI delivery modes.
  Setup uses `InitializeGoogle` and `ListTaskLists` without report settings.

The desktop adapter retains one service instance and an opaque prepared snapshot;
the frontend receives only preview data and an unguessable preparation ID. A send
attempt consumes the snapshot, even if authorization fails, so retries require a
deliberate new preparation. Input revisions reject late preparation results after
edits, and duplicate send calls cannot claim the same snapshot.

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
