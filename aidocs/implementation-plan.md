# TOML configuration, CLI setup, and recipient email delivery

## Goal and agreed scope

Move report settings out of `main.go` and into TOML. `start_date`, `end_date`, and `window_days` define a series of report periods. For each due period, a run reads one Google task list, builds a separate personalized email for every configured recipient, then sends those emails concurrently. A failed send does not prevent attempts for the other recipients. The run ends with a recipient-by-recipient result report and a nonzero exit status if any send failed.

This release uses Google-issued task list IDs. `--init` explicitly performs OAuth setup, and `--list-task-lists` lets users discover an ID by its familiar list name. Configuring by list name and supporting different task lists or intervals per recipient are future options.

## Configuration

Proposed default path: `config.toml` in the existing `weekly-tasks` user configuration directory, alongside `credentials.json` and `token.json`. The file contains settings, not OAuth credentials or tokens.

Configure the first report date, final report date, and number of calendar days in each report:

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

All three scheduling fields are required. Dates are quoted ISO calendar dates (`YYYY-MM-DD`), and `end_date` is inclusive. Starting at `start_date`, successive windows each cover `window_days` days; shorten the last window at `end_date`. The example produces Sep 19–25, Sep 26–Oct 2, and Oct 3–5. Reject invalid dates, `window_days < 1`, and an end date before the start date.

On each invocation, calculate the window containing today's local calendar date. Before `start_date` and after `end_date`, exit successfully without sending. Within a window, send its report once, even if the program is invoked repeatedly. If an entire window is missed, skip that expired window and send only the current one; an old “upcoming tasks” report would no longer be timely.

Extend the existing `Config` and `Recipient` fields and TOML tags with the two date strings. Add a TOML loader and validate before making any send request. Require a nonempty `task_list_id` for report runs, removing the current fallback to the account's first list. Reject unknown TOML keys so a misspelled setting cannot silently change who receives a report or which list is used. Validation should continue to check sender, subject, schedule fields, recipient names, and recipient addresses; it should identify the failing field or recipient.

[`BurntSushi/toml`](https://github.com/BurntSushi/toml) is a suitable decoder for these existing struct tags. Its metadata reports [undecoded keys](https://github.com/BurntSushi/toml/blob/master/decode.go), which supports the typo check. The implementation should add a dependency only when the feature is coded.

## CLI behavior

| Invocation | Behavior |
| --- | --- |
| `weekly-report --init` | Use `credentials.json` to run the interactive OAuth flow and save or replace `token.json`; do not require report TOML or send email. |
| `weekly-report --list-task-lists` | Use the saved OAuth token, print each list title and ID, then exit without reading report TOML or sending email. |
| `weekly-report` | Load and validate TOML, check whether the current report window needs delivery, then send and print results if due. Otherwise exit without sending. |
| `weekly-report --help` | Show flags, the setup order, and where to find `config.toml`. |

Example list output:

```text
TITLE                 ID
Personal              MDEyMzQ1Njc4OQ
Work                  OTI4MzQ1Njc4OQ
```

Both setup flags must branch before report configuration is loaded or validated. Use Kong's mutually exclusive (`xor`) flag group to reject `--init` and `--list-task-lists` together. The normal report path and list path should never start the interactive OAuth flow: if `token.json` is missing or unusable, they should fail with an instruction to run `weekly-report --init`. Access-token refresh can remain automatic when the saved refresh token is valid. `--init` should deliberately run authorization even if a token already exists, allowing the user to repair a revoked grant. Its existing browser-URL-and-pasted-redirect interaction can be reused. Request offline access and fresh consent during reauthorization so a replacement refresh token can be obtained; verify the returned token before replacing a working token file. [Google documents](https://developers.google.com/identity/protocols/oauth2/web-server) that refresh-token issuance normally occurs on first authorization, and Go's OAuth package provides [`ApprovalForce`](https://pkg.go.dev/golang.org/x/oauth2#ApprovalForce) for `prompt=consent`. If `task_list_id` is empty or not found, the report error should suggest `weekly-report --list-task-lists`. Continue fetching the list catalog in report mode to verify that the configured ID belongs to the authenticated account.

Use [`Kong`](https://github.com/alecthomas/kong) for CLI parsing now. Model `--init` and `--list-task-lists` as explicit root flags, with normal sending as the no-flag action. Kong supports flags, commands, and subcommands, so future actions can become named commands without replacing the parser. Keep CLI parsing separate from TOML decoding and Google API logic; Kong parses how the program runs, while the TOML decoder reads report settings. Add the Kong dependency when implementation begins.

### CLI growth

Future independent actions such as `validate-config`, `preview`, or `status` can become Kong commands. If `send` later becomes an explicit command, keep bare `weekly-report` as an alias or document a deliberate migration so existing scheduled invocations keep working. A future `--config` path can override the default config location without changing setup mode behavior.

## Report dates

Replace every “this week” phrase with the active window's inclusive range, for example, “You have 3 tasks due from Sep 19, 2026 to Sep 25, 2026.” Apply the same wording to the singular and empty report states. Calculate the active window once, pass its start and end dates to the renderer, and use those same dates for the Google Tasks query. Formatting both dates with a year also handles ranges that cross a year boundary.

Google Tasks records due dates as dates; its API [discards the time portion](https://developers.google.com/workspace/tasks/reference/rest/v1/tasks). Use the machine's local calendar date to select the active window, then convert the window's calendar bounds to the query convention used by Google Tasks. Test runs near midnight and across month or year boundaries so the displayed range matches the queried dates.

## External scheduling and delivery state

An external scheduler can invoke the program regularly. The app determines which `window_days` period is due. This implementation does not include a LaunchAgent, plist, setup script, or scheduler installation flow. `--init` stays a manual setup step, so unattended report runs never wait for browser input.

Because the program may run repeatedly within one window, persist per-window, per-recipient delivery state in the user application directory. Include the task list ID and recipient address in each state key. Coordinate concurrent result updates and prevent overlapping processes from starting the same send batch. Record an attempt before each Gmail call and its result afterward; an interrupted attempt remains uncertain and must not be retried automatically. A later run should skip recipients already attempted for that window and keep failures visible for manual follow-up. This limits duplicates but cannot guarantee exactly-once delivery when a network result is ambiguous.

## Build and send flow

1. Parse the CLI mode with Kong. Split the current `createGoogleClient` behavior in `auth.go` so only `--init` runs interactive authorization and saves the token. `--list-task-lists` loads the saved token, creates the Tasks service, and calls `GetTaskLists`.
2. In report mode, load and validate TOML. Determine the current anchored window and consult delivery state. Exit without sending if outside the configured schedule or if all recipients were already attempted for this window.
3. Load the saved token without an interactive fallback. Resolve and verify the configured task list ID; fetch and sort tasks for the active window once.
4. Render and prepare `OutgoingEmail` values for recipients who have not been attempted in the active window. Refactor raw-message construction out of `SendEmail` so every pending message can be built and validated before any send request. If preparation fails, abort without sending.
5. Send prepared emails with a small, bounded number of in-flight requests (initially four). Record each recipient's attempt durably just before its Gmail call. Each request retains its recipient identity and produces a result. Wait for all attempts to finish; cancellation should mark queued, unattempted entries as skipped.
6. Persist each completed attempt's outcome. Print results in configuration order after all sends finish, regardless of completion order. Include each recipient address, success message ID or error, and total counts. Return an error when any send fails so scheduled runs can detect partial failure.

Gmail's send endpoint accepts one message per request, so concurrent requests do not make the batch atomic. A timeout may occur after Gmail accepted a message. Do not automatically retry an uncertain send, since that could create a duplicate. The failure report should make manual follow-up possible without resending the entire batch blindly.

Example final output:

```text
SENT    alice@example.com  message ID 18fabc...
FAILED  bob@example.com    send Gmail message: ...
1 sent, 1 failed
```

## Implementation checks

- TOML decoding accepts multiple `[[recipients]]` entries and the combined `start_date`, `end_date`, `window_days` schedule. It rejects missing IDs, malformed addresses, unknown keys, missing or invalid schedule fields, and reversed dates.
- Kong rejects conflicting setup flags and produces help for each current mode. `--init` works without `config.toml`, preserves an existing token on failed authorization, and saves a usable refresh token on success; the listing and report paths never trigger interactive authorization.
- `--list-task-lists` prints the title/ID mapping without requiring `config.toml` or calling Gmail. A missing token points users to `--init`.
- Window selection covers the first, middle, and shortened final windows; runs before the start or after the end do nothing. A delayed run within a window sends that window, and a later run does not repeat attempted recipients.
- Process locking and delivery state prevent ordinary duplicate sends from repeated or overlapping invocations; an expired window is skipped.
- Batch preparation creates one correctly addressed, personalized message per recipient and sends nothing when rendering or message construction fails.
- A fake Gmail endpoint confirms multiple sends can overlap, one failure does not cancel the others, and the final report and exit status reflect every outcome.
- Rendered text shows the same inclusive active window used to fetch tasks, including the empty state and month or year boundaries.

Tests should use fake API endpoints; they must not send real email. Update the README with the recurring TOML schedule, config location, `--init`, the list-discovery command, and send result behavior.
