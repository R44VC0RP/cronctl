# cronctl

`cronctl` is a cross-platform, agent-friendly task scheduler. It provides one
predictable CLI and hides the user-level `launchd`, `systemd`, or Windows logon
configuration needed to keep schedules running.

This is an early release and its storage schema may still evolve.

## Why this shape

- Jobs belong to `cronctl`, not to three incompatible OS schedulers.
- One unprivileged per-user daemon handles scheduling on every platform.
- Commands are argv arrays by default, avoiding shell injection and quoting surprises.
- Every command supports `--json`, fixed error codes, and non-interactive use.
- Job files are independent, atomic JSON documents with stable IDs.
- Timezones work on Windows because the IANA database is embedded.
- Laptops use an explicit `catchup: skip|once` policy after sleep or scheduler downtime. The tool does not wake a sleeping machine.

## Install

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/R44VC0RP/cronctl/main/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/R44VC0RP/cronctl/main/install.ps1 | iex
```

Both installers download the matching archive from the latest GitHub Release
and verify it against the published `SHA256SUMS` before installing. Unix uses
`/usr/local/bin` when it is writable and otherwise uses `~/.local/bin`. Windows
uses `%LOCALAPPDATA%\Programs\cronctl` and adds it to the user `PATH`.

Install a specific version or choose another directory with environment
variables:

```sh
CRONCTL_VERSION=v0.2.0 CRONCTL_INSTALL_DIR="$HOME/bin" \
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/R44VC0RP/cronctl/main/install.sh)"
```

```powershell
$env:CRONCTL_VERSION = "v0.2.0"
$env:CRONCTL_INSTALL_DIR = "$HOME\bin"
irm https://raw.githubusercontent.com/R44VC0RP/cronctl/main/install.ps1 | iex
```

Installation does not enable background scheduling automatically. Inspect and
install the per-user service explicitly:

```sh
cronctl service install --dry-run
cronctl service install
```

## Build From Source

Go 1.24 or newer is required.

```sh
make build
./bin/cronctl version
```

`VERSION=v0.2.0 make release` builds checksum-verified archives for macOS,
Linux, and Windows on arm64 and amd64. Windows includes `cronctl-daemon.exe`,
built as a GUI-subsystem binary so it can start at logon without flashing a
console window. Keep it next to `cronctl.exe`.

Pushing a semantic-version tag such as `v0.2.0` runs tests, builds the same
archives, creates provenance attestations, and publishes a GitHub Release with
generated notes.

## Quick Start

```sh
cronctl validate --cron "0 9 * * *" --timezone America/New_York

cronctl add morning-report \
  --cron "0 9 * * *" \
  --timezone America/New_York \
  --missed once \
  --timeout 10m \
  --inherit-path \
  -- /absolute/path/to/report --format concise

cronctl list
cronctl run morning-report
cronctl next morning-report
cronctl runs morning-report
cronctl logs morning-report
cronctl why morning-report
cronctl service install
cronctl doctor
```

An executable script is simply a command entry point:

```sh
cronctl add cleanup --every 6h -- /absolute/path/to/cleanup.sh
```

On Windows, use an explicit interpreter when appropriate:

```powershell
cronctl add cleanup --every 6h -- powershell.exe -File C:\Tasks\cleanup.ps1
```

No shell is inserted implicitly. If shell syntax is needed, invoke the shell
explicitly (`sh -c ...`, `cmd.exe /C ...`, or `powershell.exe -Command ...`).

## Commands

```text
cronctl add NAME (--every DURATION | --cron EXPR | --schedule SCHEDULE) [options] -- COMMAND [ARG...]
cronctl list|ls
cronctl show|get NAME
cronctl rm|remove NAME
cronctl pause|disable NAME
cronctl resume|enable NAME
cronctl validate (--every DURATION | --cron EXPR | --schedule SCHEDULE) [--timezone ZONE]
cronctl run NAME
cronctl next [NAME] [--count N]
cronctl runs|history [NAME] [--limit N]
cronctl logs NAME [--run RUN_ID] [--tail BYTES]
cronctl why NAME
cronctl export [NAME...]
cronctl apply -f FILE [--dry-run]
cronctl status
cronctl doctor
cronctl capabilities
cronctl daemon run
cronctl service install|status|uninstall [--dry-run]
```

Accepted schedules are five-field cron plus a deliberately small readable grammar:

```text
every 15m
daily at 09:00
weekly on mon at 09:00
0 9 * * MON-FRI
```

Use `cronctl validate --json` to get the canonical form and the next five run
times before creating a job.

`--schedule` remains supported for readable forms such as `daily at 09:00`.
`--every` and `--cron` are the preferred unambiguous forms for automation.

## Missed Runs

Every recurring job has an explicit policy for occurrences missed while the
machine sleeps or the scheduler is stopped:

```sh
cronctl add cache-refresh --every 15m --missed skip -- ./refresh
cronctl add daily-backup --cron "0 2 * * *" --missed once -- ./backup
```

`skip` records one aggregated missed-run decision without executing. `once`
coalesces all missed occurrences into one catch-up run. The daemon persists its
next-fire cursor before execution, so sleep and daemon restarts behave the same.

## Declarative Jobs

Exported manifests contain portable job definitions without runtime IDs or
timestamps. They are JSON-only in this experiment.

```sh
cronctl export > jobs.json
cronctl apply -f jobs.json --dry-run
cronctl apply -f jobs.json
cronctl export --json | cronctl apply -f - --dry-run --json
```

Apply validates every job before writing, upserts by exact name, and reports
created, updated, and unchanged jobs. It never deletes omitted jobs. Environment
values are included for round-tripping, so treat exported manifests as secrets.

## Agent Contract

- `--json` may appear anywhere and emits exactly one JSON object on stdout.
- JSON responses include `schema_version`.
- Error objects include stable codes such as `JOB_NOT_FOUND`, `JOB_CONFLICT`, and `VALIDATION`.
- Exit codes: `2` usage, `3` not found, `4` conflict/running, `5` validation, `6` service unavailable.
- `cronctl run` propagates the job's exit code.
- `add` is idempotent: an identical named job is unchanged; a conflicting job requires `--replace`.
- Successful `add` output includes the next three fires, scheduler readiness, and an actionable warning when the service is stopped.
- `export | apply` is idempotent and `apply --dry-run` is non-mutating.
- Stored environment values are redacted from `get` and `list` output.

## OS Integration

- macOS: `~/Library/LaunchAgents/dev.cronctl.daemon.plist` with modern `launchctl bootstrap`.
- Linux: `~/.config/systemd/user/cronctl.service`. Running after logout may require `loginctl enable-linger $USER`; non-systemd systems can supervise `cronctl daemon run` directly.
- Windows: the unprivileged HKCU `Run` key starts `cronctl-daemon.exe` at logon. It intentionally does not use an admin-only Windows Service.

Inspect generated integration without modifying the machine:

```sh
cronctl service install --dry-run --json
```

## Storage

Set `CRONCTL_HOME` to override all paths, which is useful for portable use and
tests. Otherwise config follows the OS user config directory; state and logs use
XDG state on Linux, `~/Library/Application Support/cronctl` on macOS, and
`%LOCALAPPDATA%\cronctl` on Windows.

Output is written under `runs/` with a 10 MiB cap per run and the newest 20 runs
retained per job. History stores metadata and a bounded 16 KiB output tail, and
is compacted to its newest 1,000 records. Environment variables stored in job
definitions are sensitive, so job and state files use user-private permissions
where supported.

## Deferred Deliberately

Retries, one-shot `--at` jobs, inline managed scripts, secrets, destructive
manifest pruning, IPC, and Task Scheduler COM integration are follow-up work.
