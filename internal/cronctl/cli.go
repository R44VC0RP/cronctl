package cronctl

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const usage = `cronctl schedules commands without making you manage OS services.

Usage:
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

Schedules:
  "every 15m", "daily at 09:00", "weekly on mon at 09:00", or five-field cron.

Global:
  --json  Emit one stable JSON object; accepted anywhere.
`

type commandResult struct {
	data     any
	plain    string
	exitCode int
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func Main(args []string) int {
	jsonMode, cleaned := extractJSONMode(args)
	result, err := dispatch(cleaned)
	if err != nil {
		return printError(err, jsonMode)
	}
	if jsonMode {
		payload := map[string]any{"schema_version": schemaVersion, "data": result.data}
		_ = json.NewEncoder(os.Stdout).Encode(payload)
	} else if result.plain != "" {
		fmt.Fprintln(os.Stdout, result.plain)
	}
	return result.exitCode
}

func extractJSONMode(args []string) (bool, []string) {
	jsonMode := false
	afterSeparator := false
	cleaned := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--" {
			afterSeparator = true
			cleaned = append(cleaned, arg)
			continue
		}
		if arg == "--json" && !afterSeparator {
			jsonMode = true
			continue
		}
		cleaned = append(cleaned, arg)
	}
	return jsonMode, cleaned
}

func dispatch(args []string) (commandResult, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		return commandResult{data: map[string]string{"usage": usage}, plain: usage}, nil
	}
	if args[0] == "version" || args[0] == "--version" {
		return commandResult{data: map[string]string{"version": version}, plain: version}, nil
	}
	s, err := newStore()
	if err != nil {
		return commandResult{}, err
	}
	switch args[0] {
	case "add":
		return addCommand(s, args[1:])
	case "list", "ls":
		return listCommand(s, args[1:])
	case "get", "show":
		return getCommand(s, args[1:])
	case "remove", "rm":
		return removeCommand(s, args[1:])
	case "enable", "disable", "pause", "resume":
		return enableCommand(s, args[0], args[1:])
	case "validate":
		return validateCommand(args[1:])
	case "run":
		return runCommand(s, args[1:])
	case "history", "runs":
		return historyCommand(s, args[1:])
	case "next":
		return nextCommand(s, args[1:])
	case "logs":
		return logsCommand(s, args[1:])
	case "why":
		return whyCommand(s, args[1:])
	case "export":
		return exportCommand(s, args[1:])
	case "apply":
		return applyCommand(s, args[1:])
	case "status":
		return statusCommand(s, args[1:])
	case "doctor":
		return doctorCommand(s, args[1:])
	case "capabilities":
		return capabilitiesCommand(args[1:])
	case "daemon":
		if len(args) != 2 || args[1] != "run" {
			return commandResult{}, usageError("usage: cronctl daemon run")
		}
		return commandResult{}, s.runDaemon()
	case "service":
		return serviceCommand(s, args[1:])
	default:
		return commandResult{}, usageError("unknown command %q", args[0])
	}
}

func addCommand(s *store, args []string) (commandResult, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return commandResult{}, usageError("usage: cronctl add NAME (--every DURATION | --cron EXPR | --schedule SCHEDULE) [options] -- COMMAND [ARG...]")
	}
	name := args[0]
	fs := newFlagSet("add")
	schedule := fs.String("schedule", "", "schedule")
	every := fs.String("every", "", "fixed interval")
	cronExpression := fs.String("cron", "", "five-field cron expression")
	timezone := fs.String("timezone", "Local", "IANA timezone")
	cwd := fs.String("cwd", "", "working directory")
	timeout := fs.String("timeout", "", "maximum run duration")
	overlap := fs.String("overlap", "skip", "skip or allow")
	replace := fs.Bool("replace", false, "replace an existing job")
	inheritPath := fs.Bool("inherit-path", false, "store the current PATH")
	catchup := "skip"
	fs.StringVar(&catchup, "catchup", "skip", "missed-run policy: skip or once")
	fs.StringVar(&catchup, "missed", "skip", "alias for --catchup")
	var envValues stringList
	fs.Var(&envValues, "env", "environment variable KEY=VALUE; repeatable")
	if err := fs.Parse(args[1:]); err != nil {
		return commandResult{}, usageError("%v", err)
	}
	argv := fs.Args()
	selectedSchedule, err := selectSchedule(*schedule, *every, *cronExpression)
	if err != nil || len(argv) == 0 {
		if err != nil {
			return commandResult{}, err
		}
		return commandResult{}, usageError("a schedule and COMMAND are required")
	}
	if !namePattern.MatchString(name) {
		return commandResult{}, &cliError{Code: "INVALID_NAME", Message: "job names must match [a-z0-9][a-z0-9-]{0,62}", Exit: 5}
	}
	parsed, err := parseSchedule(selectedSchedule, *timezone)
	if err != nil {
		return commandResult{}, validationError(err)
	}
	if *overlap != "skip" && *overlap != "allow" {
		return commandResult{}, validationError(fmt.Errorf("--overlap must be skip or allow"))
	}
	if catchup != "skip" && catchup != "once" {
		return commandResult{}, validationError(fmt.Errorf("--catchup must be skip or once"))
	}
	if *timeout != "" {
		duration, err := time.ParseDuration(*timeout)
		if err != nil || duration <= 0 {
			return commandResult{}, validationError(fmt.Errorf("--timeout must be a positive duration"))
		}
	}
	if *cwd != "" {
		absolute, err := filepath.Abs(*cwd)
		if err != nil {
			return commandResult{}, validationError(err)
		}
		*cwd = absolute
		info, err := os.Stat(*cwd)
		if err != nil || !info.IsDir() {
			return commandResult{}, validationError(fmt.Errorf("--cwd must be an existing directory"))
		}
	}
	env := make(map[string]string)
	for _, item := range envValues {
		key, value, found := strings.Cut(item, "=")
		if !found || key == "" || strings.ContainsRune(key, '\x00') {
			return commandResult{}, validationError(fmt.Errorf("--env must be KEY=VALUE"))
		}
		env[key] = value
	}
	if *inheritPath {
		env["PATH"] = os.Getenv("PATH")
	}
	now := time.Now().UTC()
	job := Job{
		SchemaVersion: schemaVersion,
		ID:            newID(),
		Name:          name,
		Argv:          argv,
		Cwd:           *cwd,
		Env:           env,
		Schedule:      ScheduleSpec{Raw: selectedSchedule, Kind: parsed.kind, Canonical: parsed.canonical, Timezone: parsed.location.String()},
		Enabled:       true,
		Overlap:       *overlap,
		Catchup:       catchup,
		Timeout:       *timeout,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	action, err := s.put(job, *replace)
	if err != nil {
		return commandResult{}, err
	}
	stored, err := s.get(name)
	if err != nil {
		return commandResult{}, err
	}
	next, _ := nextTimes(stored.Schedule, time.Now(), 3)
	scheduler := schedulerReadiness(s)
	warnings := []map[string]string{}
	warnings = append(warnings, executionWarnings(stored)...)
	plain := fmt.Sprintf("%s %s\nnext: %s", action, name, next[0].Format(time.RFC3339))
	if scheduler["state"] != "running" {
		warnings = append(warnings, map[string]string{"code": "SCHEDULER_NOT_RUNNING", "message": "job is saved but will not run until `cronctl service install` starts the scheduler"})
		plain += "\nwarning: scheduler is not running; run `cronctl service install`"
	}
	data := map[string]any{"action": action, "job": redactJob(stored), "next_fires": next, "scheduler": scheduler, "warnings": warnings}
	return commandResult{data: data, plain: plain}, nil
}

func listCommand(s *store, args []string) (commandResult, error) {
	if len(args) != 0 {
		return commandResult{}, usageError("usage: cronctl list")
	}
	jobs, err := s.list()
	if err != nil {
		return commandResult{}, err
	}
	type jobView struct {
		Job
		NextFireAt *time.Time `json:"next_fire_at,omitempty"`
	}
	views := make([]jobView, len(jobs))
	lines := make([]string, 0, len(jobs)+1)
	lines = append(lines, "NAME\tENABLED\tNEXT\tSCHEDULE\tCOMMAND")
	for i, job := range jobs {
		var nextFire *time.Time
		if job.Enabled {
			next, _ := nextTimes(job.Schedule, time.Now(), 1)
			nextFire = &next[0]
		}
		views[i] = jobView{Job: redactJob(job), NextFireAt: nextFire}
		nextText := "paused"
		if nextFire != nil {
			nextText = nextFire.Format(time.RFC3339)
		}
		lines = append(lines, fmt.Sprintf("%s\t%t\t%s\t%s\t%s", job.Name, job.Enabled, nextText, job.Schedule.Raw, strings.Join(job.Argv, " ")))
	}
	return commandResult{data: views, plain: strings.Join(lines, "\n")}, nil
}

func getCommand(s *store, args []string) (commandResult, error) {
	if len(args) != 1 {
		return commandResult{}, usageError("usage: cronctl get NAME")
	}
	job, err := s.get(args[0])
	if err != nil {
		return commandResult{}, err
	}
	redacted := redactJob(job)
	next, _ := nextTimes(job.Schedule, time.Now(), 3)
	last, _ := s.history(job.Name, 1)
	type jobDetails struct {
		Job
		NextFires []time.Time    `json:"next_fires"`
		Scheduler map[string]any `json:"scheduler"`
		LastRun   *RunRecord     `json:"last_run"`
	}
	data := jobDetails{Job: redacted, NextFires: next, Scheduler: schedulerReadiness(s)}
	if len(last) == 1 {
		data.LastRun = &last[0]
	}
	return commandResult{data: data, plain: prettyJSON(data)}, nil
}

func removeCommand(s *store, args []string) (commandResult, error) {
	if len(args) != 1 {
		return commandResult{}, usageError("usage: cronctl remove NAME")
	}
	if err := s.remove(args[0]); err != nil {
		return commandResult{}, err
	}
	return commandResult{data: map[string]any{"removed": true, "name": args[0]}, plain: "removed " + args[0]}, nil
}

func enableCommand(s *store, command string, args []string) (commandResult, error) {
	if len(args) != 1 {
		return commandResult{}, usageError("usage: cronctl %s NAME", command)
	}
	enabled := command == "enable" || command == "resume"
	job, err := s.setEnabled(args[0], enabled)
	if err != nil {
		return commandResult{}, err
	}
	verb := "paused"
	if enabled {
		verb = "resumed"
	}
	return commandResult{data: redactJob(job), plain: verb + " " + args[0]}, nil
}

func validateCommand(args []string) (commandResult, error) {
	fs := newFlagSet("validate")
	schedule := fs.String("schedule", "", "schedule")
	every := fs.String("every", "", "fixed interval")
	cronExpression := fs.String("cron", "", "five-field cron expression")
	timezone := fs.String("timezone", "Local", "IANA timezone")
	count := fs.Int("count", 5, "number of future runs")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if err != nil {
			return commandResult{}, usageError("%v", err)
		}
		return commandResult{}, usageError("usage: cronctl validate (--every DURATION | --cron EXPR | --schedule SCHEDULE) [--timezone ZONE]")
	}
	if *count < 1 || *count > 20 {
		return commandResult{}, validationError(fmt.Errorf("--count must be between 1 and 20"))
	}
	selectedSchedule, err := selectSchedule(*schedule, *every, *cronExpression)
	if err != nil {
		return commandResult{}, err
	}
	parsed, err := parseSchedule(selectedSchedule, *timezone)
	if err != nil {
		return commandResult{}, validationError(err)
	}
	spec := ScheduleSpec{Raw: selectedSchedule, Kind: parsed.kind, Canonical: parsed.canonical, Timezone: parsed.location.String()}
	next, _ := nextTimes(spec, time.Now(), *count)
	data := map[string]any{"schedule": spec, "next": next}
	return commandResult{data: data, plain: prettyJSON(data)}, nil
}

func runCommand(s *store, args []string) (commandResult, error) {
	if len(args) != 1 {
		return commandResult{}, usageError("usage: cronctl run NAME")
	}
	job, err := s.get(args[0])
	if err != nil {
		return commandResult{}, err
	}
	record, runErr := s.run(job, "manual")
	if runErr != nil && record.Status == "" {
		return commandResult{}, runErr
	}
	var cliErr *cliError
	if errors.As(runErr, &cliErr) && cliErr.Code == "JOB_RUNNING" {
		return commandResult{}, runErr
	}
	return commandResult{data: record, plain: prettyJSON(record), exitCode: record.ExitCode}, nil
}

func historyCommand(s *store, args []string) (commandResult, error) {
	name := ""
	flagArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		flagArgs = args[1:]
	}
	fs := newFlagSet("history")
	limit := fs.Int("limit", 20, "maximum records")
	if err := fs.Parse(flagArgs); err != nil || fs.NArg() != 0 || *limit < 1 || *limit > 1000 {
		return commandResult{}, usageError("usage: cronctl runs [NAME] [--limit 1..1000]")
	}
	var records []RunRecord
	var err error
	if name == "" {
		records, err = s.allHistory(*limit)
	} else {
		records, err = s.history(name, *limit)
	}
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{data: records, plain: prettyJSON(records)}, nil
}

func statusCommand(s *store, args []string) (commandResult, error) {
	if len(args) != 0 {
		return commandResult{}, usageError("usage: cronctl status")
	}
	heartbeat, running, err := daemonStatus(s)
	if err != nil {
		return commandResult{}, err
	}
	data := map[string]any{"running": running, "heartbeat": heartbeat}
	jobs, listErr := s.list()
	if listErr == nil {
		enabled := 0
		for _, job := range jobs {
			if job.Enabled {
				enabled++
			}
		}
		data["jobs"] = map[string]int{"total": len(jobs), "enabled": enabled}
	}
	data["scheduler"] = schedulerReadiness(s)
	plain := "daemon stopped"
	if running {
		plain = fmt.Sprintf("daemon running (pid %d, last tick %s)", heartbeat.PID, heartbeat.LastTick.Format(time.RFC3339))
	}
	return commandResult{data: data, plain: plain}, nil
}

func doctorCommand(s *store, args []string) (commandResult, error) {
	if len(args) != 0 {
		return commandResult{}, usageError("usage: cronctl doctor")
	}
	service, err := platformServiceStatus(s.paths)
	if err != nil {
		service.Detail = err.Error()
	}
	_, daemonRunning, daemonErr := daemonStatus(s)
	checks := []map[string]any{
		{"name": "config_writable", "ok": writable(s.paths.config), "path": s.paths.config},
		{"name": "state_writable", "ok": writable(s.paths.state), "path": s.paths.state},
		{"name": "service_installed", "ok": service.Installed, "detail": service.Detail},
		{"name": "daemon_running", "ok": daemonRunning, "detail": errorString(daemonErr)},
		{"name": "path", "ok": os.Getenv("PATH") != "", "value": os.Getenv("PATH")},
	}
	ok := true
	for _, check := range checks {
		if value, _ := check["ok"].(bool); !value {
			ok = false
		}
	}
	data := map[string]any{"ok": ok, "checks": checks}
	return commandResult{data: data, plain: prettyJSON(data)}, nil
}

func serviceCommand(s *store, args []string) (commandResult, error) {
	if len(args) == 0 {
		return commandResult{}, usageError("usage: cronctl service install|status|uninstall")
	}
	switch args[0] {
	case "install":
		fs := newFlagSet("service install")
		dryRun := fs.Bool("dry-run", false, "print without installing")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return commandResult{}, usageError("usage: cronctl service install [--dry-run]")
		}
		executable, err := os.Executable()
		if err != nil {
			return commandResult{}, err
		}
		plan, err := platformServiceInstall(s.paths, executable, *dryRun)
		if err != nil {
			return commandResult{}, serviceError("install", err)
		}
		verb := "installed"
		if *dryRun {
			verb = "dry run"
		}
		return commandResult{data: map[string]any{"action": verb, "plan": plan}, plain: verb + "\n" + prettyJSON(plan)}, nil
	case "status":
		if len(args) != 1 {
			return commandResult{}, usageError("usage: cronctl service status")
		}
		state, err := platformServiceStatus(s.paths)
		if err != nil {
			return commandResult{}, serviceError("status", err)
		}
		_, running, _ := daemonStatus(s)
		state.Running = running
		return commandResult{data: state, plain: prettyJSON(state)}, nil
	case "uninstall":
		if len(args) != 1 {
			return commandResult{}, usageError("usage: cronctl service uninstall")
		}
		if err := platformServiceUninstall(s.paths); err != nil {
			return commandResult{}, serviceError("uninstall", err)
		}
		return commandResult{data: map[string]bool{"uninstalled": true}, plain: "uninstalled"}, nil
	default:
		return commandResult{}, usageError("unknown service command %q", args[0])
	}
}

func redactJob(job Job) Job {
	if len(job.Env) == 0 {
		return job
	}
	redacted := make(map[string]string, len(job.Env))
	keys := make([]string, 0, len(job.Env))
	for key := range job.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		redacted[key] = "***"
	}
	job.Env = redacted
	return job
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func usageError(format string, args ...any) error {
	return &cliError{Code: "USAGE", Message: fmt.Sprintf(format, args...), Exit: 2}
}

func validationError(err error) error {
	return &cliError{Code: "VALIDATION", Message: err.Error(), Exit: 5}
}

func selectSchedule(legacy, every, cronExpression string) (string, error) {
	provided := 0
	for _, value := range []string{legacy, every, cronExpression} {
		if value != "" {
			provided++
		}
	}
	if provided != 1 {
		return "", usageError("provide exactly one of --every, --cron, or --schedule")
	}
	if every != "" {
		duration, err := time.ParseDuration(every)
		if err != nil || duration <= 0 {
			return "", validationError(fmt.Errorf("--every must be a positive duration"))
		}
		return "every " + every, nil
	}
	if cronExpression != "" {
		return cronExpression, nil
	}
	return legacy, nil
}

func printError(err error, jsonMode bool) int {
	code := 1
	errorCode := "INTERNAL"
	var cliErr *cliError
	if errors.As(err, &cliErr) {
		code = cliErr.Exit
		errorCode = cliErr.Code
	}
	if jsonMode {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema_version": schemaVersion, "error": map[string]string{"code": errorCode, "message": err.Error()}})
	} else {
		fmt.Fprintln(os.Stderr, "cronctl:", err)
	}
	return code
}

func prettyJSON(value any) string {
	data, _ := json.MarshalIndent(value, "", "  ")
	return string(data)
}

func writable(path string) bool {
	f, err := os.CreateTemp(path, ".write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
