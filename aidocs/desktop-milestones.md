# Wails desktop development roadmap

## Goal and scope

Make Weekly Report usable without a terminal: install the app, connect Google,
choose a task list, configure recipients, preview a personalized report, and send
it with clear per-recipient results. Start with one account, one task list, and
the existing report template. Retain the CLI for automation.

The initial platform is macOS on Apple Silicon. Use Wails v2.16.0 and a
React/TypeScript frontend. Keep report rules, Google access, configuration, and
delivery policy in Go. `desktop/` owns native startup and frontend bindings;
`internal/app` provides shared operations; `internal/report` renders and selects
dates; `internal/google` integrates with Google APIs.

## Milestones

| Milestone | Deliverable | Acceptance criteria |
| --- | --- | --- |
| 1. Desktop feasibility | Minimal Wails app, welcome screen, Go connection check, build instructions | Development startup, production build, and packaged launch work on the initial target OS. |
| 2. Shared application service | Structured settings, report preparation, and delivery operations shared by CLI and future desktop workflows | CLI behavior and existing tests remain intact; preview cannot send; prepared content matches sent content; recipient results are available without parsing terminal output. |
| 3. Onboarding and settings | System-browser Google connection, task-list picker, sender/subject and recipient editor | A new user can configure the app without editing TOML; validation and reconnect errors are understandable; existing users can retain settings. |
| 4. Preview and manual send | Date presets/custom range, personalized preview, progress, per-recipient results | Editing inputs invalidates the displayed preparation; send uses the reviewed snapshot; repeated clicks cannot start duplicate sends; offline/auth/partial-delivery cases work. **Implemented.** |
| 5. Scheduling | Daily local-time schedule editor and macOS LaunchAgent background runner | Window closure, sleep, restarts, missed windows, and overlapping runs have defined and tested behavior. **Implemented for macOS.** |
| 6. Public release | Installer, signing/notarization, polished onboarding and recovery | A nontechnical tester can install, connect, preview, and send unaided; keyboard navigation, labels, focus, contrast, and errors are checked. |

## Delivery rules to preserve

- Manual ranges are independent of the configured schedule. Each CLI manual
  invocation intentionally sends again and never reads or writes scheduled state.
- Scheduled sends use fixed inclusive windows and record attempts before Gmail.
  Sent, failed, and uncertain attempts are not automatically retried.
- Expired windows are skipped. A batch may partially succeed; report every
  recipient's outcome in configuration order.
- Desktop scheduled delivery runs once daily at a saved local time. It uses a
  per-user macOS LaunchAgent and the headless runner inside the installed app
  bundle; the GUI can close while the user remains logged in.
- A launchd calendar event missed during sleep runs on wake. If the Mac is powered
  off, launchd waits for the next daily event. Reopening Weekly Report after the
  configured time catches up only when a report window is still active. Expired
  windows are not replayed.
- Scheduled sends use the existing process lock and per-recipient window state.
  Overlaps cannot send twice; failed and uncertain attempts are never retried.
- Prepare every message before starting delivery. Preview has no send side effects.
- Tokens and Google access stay in Go. Isolate preview HTML from the Wails bridge.

These rules reflect the current CLI/README and supersede the older forced-send
proposal in `implementation-plan.md` where that proposal differs.

## Milestone 2 implementation contract

- `app.NewService(dir)` supports an explicit configuration directory;
  `app.NewDefaultService()` uses the existing OS `weekly-tasks` directory.
- `LoadSettings` and `SaveSettings` share validation and persist TOML atomically.
- `ListTaskLists` and `InitializeGoogle` support the CLI setup modes without
  requiring report configuration. Desktop browser interaction follows in milestone 3.
- `PrepareReport` validates a configuration and manual range, reads tasks, renders
  personalized HTML, and builds an opaque snapshot. `Preview()` returns detached
  presentation data, never tokens or raw MIME messages.
- `SendPreparedReport` sends that snapshot once and returns structured results.
  A new preparation is required after a send attempt, including uncertain results.
  Callers must prepare again after editing inputs; the desktop adapter discards its
  old handle when inputs change and accepts only a current opaque preparation ID.
  JavaScript receives detached preview data, never the prepared message snapshot.
- `SendManualReport` and `SendScheduledReport` coordinate the existing CLI modes.
  Only the CLI adapter formats results as text. Scheduled locking/deduplication
  remains separate from manual snapshot consumption.
- Test with fake Google HTTP responses and temporary directories. Cover preview
  isolation, immutable content, single-use snapshots, partial outcomes, cancellation,
  settings round trips, invalid-save preservation, and existing CLI regressions.

## Release work to start early

Public onboarding needs a developer-managed Google Cloud project/consent screen
and applicable OAuth verification, rather than requiring each user to supply
`credentials.json`. Keep the existing PKCE/loopback flow and use the system browser.
Plan token migration to an OS credential store before public release.

For automatic delivery while the window is closed, retain a headless runner and
manage an OS scheduler integration. Delivery still depends on the machine being
available. Avoid implying that a foreground timer provides unattended delivery.

Before Windows support, replace or isolate `syscall.Flock` in delivery state.
Validate each additional OS independently. The milestone 1 macOS bundle is locally
ad-hoc signed, not a Developer ID signed/notarized public release.

## Progress

- Milestone 1 implemented: macOS arm64 production bundle and Go connection checked;
  development server starts and processes frontend hot updates; Go tests and CLI build pass.
- Milestone 2 implemented: shared service extraction and regression verification.
- Milestone 3 implemented: desktop settings editor, task-list picker, system-browser
  OAuth, and reconnect/error states. Sign-in still requires a developer/user supplied
  Google Desktop OAuth `credentials.json` at the displayed app data path; distributing
  developer-managed OAuth credentials and completing applicable Google verification
  remain release work.
- Milestone 4 implemented: manual date presets and custom inclusive dates, isolated
  personalized previews, snapshot-bound one-time sends, live progress, and ordered
  recipient outcomes. Input revisions and opaque preparation IDs reject stale or
  duplicate sends. Authorization failures produce skipped recipient outcomes; Gmail
  partial and uncertain delivery results remain visible without automatic retries.
- Milestone 5 implemented for macOS: the settings editor configures daily local-time
  delivery; an atomic per-user LaunchAgent plist invokes a headless mode in the app
  bundle. The GUI removes or updates only its own LaunchAgent, verifies the active
  plist matches the selected time and app path, and retains the latest scheduled
  results for display. Startup catch-up requires a still-active report window.
  LaunchAgent installation and rollback, time selection, restart catch-up, and
  existing overlapping/uncertain delivery protections are covered by tests.
- Milestone 6 remains planned.
