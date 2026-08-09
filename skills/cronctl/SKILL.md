---
name: cronctl
description: Schedule, inspect, run, and troubleshoot recurring local tasks with the cross-platform cronctl CLI on macOS, Linux, and Windows. Use when the user asks to schedule commands or scripts, replace cron/launchd/systemd/Task Scheduler setup, handle missed runs after sleep, inspect run history or logs, or manage cronctl jobs declaratively.
license: MIT
compatibility: Requires the cronctl CLI. Background scheduling uses an explicitly installed per-user service.
metadata:
  author: R44VC0RP
  version: "0.2.0"
---

# cronctl

Use `cronctl` for recurring local command execution without editing native OS
service definitions. Prefer its structured, non-interactive interface over
manually configuring cron, launchd, systemd, or Windows Task Scheduler.

## First Check

1. Run `cronctl capabilities --json` to confirm the CLI and supported features.
2. Run `cronctl status --json` before claiming that scheduled jobs are active.
3. If the CLI is absent, tell the user and ask before installing executable
   software. Use only the official commands:

```sh
curl -fsSL https://raw.githubusercontent.com/R44VC0RP/cronctl/main/install.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/R44VC0RP/cronctl/main/install.ps1 | iex
```

Do not install or enable the scheduler service without explicit user approval.
It creates login persistence for the current user.

## Schedule A Job

1. Translate the request into one explicit schedule form.
2. Validate it and show the next occurrences.
3. Add the named job using an argv command after `--`.
4. Confirm the resulting next fires and scheduler state from JSON output.
5. If scheduling is not active, ask before running `cronctl service install`.

Use `--every` for fixed intervals:

```sh
cronctl validate --every 15m --json
cronctl add cache-refresh --every 15m --missed skip --json -- /absolute/path/to/refresh
```

Use `--cron` for five-field calendar schedules:

```sh
cronctl validate --cron "0 9 * * MON-FRI" --timezone America/New_York --json
cronctl add weekday-report \
  --cron "0 9 * * MON-FRI" \
  --timezone America/New_York \
  --missed once \
  --timeout 10m \
  --json \
  -- /absolute/path/to/report --format concise
```

`--missed skip` records missed occurrences without running them. `--missed
once` coalesces occurrences missed during sleep or scheduler downtime into one
catch-up run.

## Command Execution

- Everything after `--` is the exact argv array.
- No shell is inserted implicitly. Invoke `sh -c`, `cmd.exe /C`, or
  `powershell.exe -Command` only when shell syntax is genuinely required.
- Prefer absolute executable and script paths for background jobs.
- Use `--cwd`, repeated `--env KEY=VALUE`, and `--inherit-path` when needed.
- Stored environment values are sensitive even though normal display commands
  redact them.

## Inspect And Troubleshoot

Use these before changing a job:

```sh
cronctl list --json
cronctl show JOB --json
cronctl next JOB --count 5 --json
cronctl why JOB --json
cronctl runs JOB --limit 20 --json
cronctl logs JOB --json
cronctl doctor --json
```

Use `cronctl run JOB --json` for a foreground manual run. It uses the same
executor, overlap lock, timeout handling, logs, and history as scheduled runs;
it does not require the daemon. Its process exit code is propagated.

## Safe Changes

- `cronctl add` is idempotent only when the named definition is identical.
- A different existing definition returns `JOB_CONFLICT`. Inspect it before
  using `--replace`; do not replace automatically.
- Prefer `cronctl pause JOB` over deletion when the user's intent may be
  temporary. Resume with `cronctl resume JOB`.
- Get explicit approval before `cronctl rm`, `cronctl service install`, or
  `cronctl service uninstall`.
- Put `--json` before the command separator. A child command may independently
  receive its own `--json` after `--`.

## Declarative Jobs

Use export/apply for repeatable multi-job configuration:

```sh
cronctl export > cronctl-jobs.json
cronctl apply -f cronctl-jobs.json --dry-run --json
cronctl apply -f cronctl-jobs.json --json
```

Always run `apply --dry-run` first and summarize created, updated, and unchanged
jobs. Apply never deletes omitted jobs. Exports include environment values for
round-tripping, so treat exported files as secrets and do not print them unless
asked.

## Current Boundaries

Do not invent flags or imply support for one-shot `--at` jobs, retries,
dependency workflows, remote execution, secret storage, destructive manifest
pruning, or machine wake. Run `cronctl capabilities --json` when behavior is in
doubt.
