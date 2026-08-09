package cronctl

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func schedulerReadiness(s *store) map[string]any {
	heartbeat, running, heartbeatErr := daemonStatus(s)
	service, serviceErr := platformServiceStatus(s.paths)
	state := "not_installed"
	action := "cronctl service install"
	if running {
		state = "running"
		action = ""
	} else if service.Installed {
		state = "stopped"
		action = "cronctl service install"
	}
	detail := service.Detail
	if serviceErr != nil {
		detail = serviceErr.Error()
	}
	if heartbeatErr != nil {
		detail = heartbeatErr.Error()
	}
	return map[string]any{
		"state":          state,
		"runnable":       running,
		"last_heartbeat": heartbeat.LastTick,
		"service":        service,
		"detail":         detail,
		"action":         action,
	}
}

func executionWarnings(job Job) []map[string]string {
	var warnings []map[string]string
	executable := job.Argv[0]
	if filepath.IsAbs(executable) || strings.ContainsAny(executable, `/\`) {
		if _, err := os.Stat(executable); err != nil {
			warnings = append(warnings, map[string]string{"code": "EXECUTABLE_NOT_FOUND", "message": fmt.Sprintf("command %q does not exist yet", executable)})
		}
		return warnings
	}
	if _, err := exec.LookPath(executable); err != nil {
		warnings = append(warnings, map[string]string{"code": "EXECUTABLE_NOT_FOUND", "message": fmt.Sprintf("command %q is not on the current PATH; use an absolute path or --inherit-path", executable)})
	}
	return warnings
}

func capabilitiesCommand(args []string) (commandResult, error) {
	if len(args) != 0 {
		return commandResult{}, usageError("usage: cronctl capabilities")
	}
	data := map[string]any{
		"version":  version,
		"platform": runtime.GOOS,
		"features": []string{
			"schedule.cron", "schedule.every", "catchup.skip", "catchup.once",
			"overlap.skip", "overlap.allow", "manifest.export", "manifest.apply",
			"output.json.v1", "service.user",
		},
		"limits": map[string]any{
			"history_records": 1000, "run_log_bytes": outputLogLimit,
			"retained_run_logs": maxRunFiles, "misfire_scan": 10_000,
		},
		"exit_codes": map[string]int{
			"ok": 0, "internal": 1, "usage": 2, "not_found": 3,
			"conflict": 4, "validation": 5, "service": 6,
			"timeout": 124, "exec_failed": 127,
		},
	}
	return commandResult{data: data, plain: prettyJSON(data)}, nil
}

func nextCommand(s *store, args []string) (commandResult, error) {
	name := ""
	flagArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		flagArgs = args[1:]
	}
	fs := newFlagSet("next")
	count := fs.Int("count", 5, "number of occurrences")
	if err := fs.Parse(flagArgs); err != nil || fs.NArg() != 0 || *count < 1 || *count > 20 {
		return commandResult{}, usageError("usage: cronctl next [NAME] [--count 1..20]")
	}
	var jobs []Job
	if name != "" {
		job, err := s.get(name)
		if err != nil {
			return commandResult{}, err
		}
		jobs = []Job{job}
	} else {
		var err error
		jobs, err = s.list()
		if err != nil {
			return commandResult{}, err
		}
	}
	now := time.Now()
	results := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		occurrences := []time.Time{}
		if job.Enabled {
			occurrences, _ = nextTimes(job.Schedule, now, *count)
		}
		results = append(results, map[string]any{"name": job.Name, "enabled": job.Enabled, "occurrences": occurrences})
	}
	data := map[string]any{"as_of": now.UTC(), "jobs": results}
	return commandResult{data: data, plain: prettyJSON(data)}, nil
}

func whyCommand(s *store, args []string) (commandResult, error) {
	if len(args) != 1 {
		return commandResult{}, usageError("usage: cronctl why NAME")
	}
	job, err := s.get(args[0])
	if err != nil {
		return commandResult{}, err
	}
	state := "waiting"
	reasons := []map[string]string{}
	next, _ := nextTimes(job.Schedule, time.Now(), 1)
	if !job.Enabled {
		state = "paused"
		reasons = append(reasons, map[string]string{"code": "JOB_PAUSED", "message": "the job is paused; run `cronctl resume " + job.Name + "`"})
	} else {
		reasons = append(reasons, map[string]string{"code": "BEFORE_NEXT_FIRE", "message": "the next scheduled occurrence is " + next[0].Format(time.RFC3339)})
	}
	scheduler := schedulerReadiness(s)
	if scheduler["state"] != "running" {
		state = "scheduler_unhealthy"
		reasons = append(reasons, map[string]string{"code": "SCHEDULER_NOT_RUNNING", "message": "the scheduler is not running"})
	}
	last, _ := s.history(job.Name, 1)
	var lastRun any
	if len(last) == 1 {
		lastRun = last[0]
	}
	data := map[string]any{"job": job.Name, "state": state, "reasons": reasons, "next_scheduled_at": next[0], "last_run": lastRun, "scheduler": scheduler}
	return commandResult{data: data, plain: prettyJSON(data)}, nil
}

func logsCommand(s *store, args []string) (commandResult, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return commandResult{}, usageError("usage: cronctl logs NAME [--run RUN_ID] [--tail BYTES]")
	}
	name := args[0]
	fs := newFlagSet("logs")
	runID := fs.String("run", "", "run ID")
	tailBytes := fs.Int64("tail", 64*1024, "maximum bytes")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *tailBytes < 1 || *tailBytes > outputLogLimit {
		return commandResult{}, usageError("usage: cronctl logs NAME [--run RUN_ID] [--tail 1..10485760]")
	}
	records, err := s.history(name, 1000)
	if err != nil {
		return commandResult{}, err
	}
	var selected *RunRecord
	for i := range records {
		if (*runID == "" || records[i].RunID == *runID) && records[i].LogPath != "" {
			selected = &records[i]
			break
		}
	}
	if selected == nil {
		return commandResult{}, &cliError{Code: "RUN_NOT_FOUND", Message: "no matching run with retained logs", Exit: 3}
	}
	output, truncated, err := readTail(selected.LogPath, *tailBytes)
	if err != nil {
		return commandResult{}, err
	}
	data := map[string]any{"job": name, "run_id": selected.RunID, "output": output, "truncated": truncated}
	return commandResult{data: data, plain: output}, nil
}

func readTail(path string, limit int64) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	truncated := info.Size() > limit
	if truncated {
		if _, err := f.Seek(-limit, io.SeekEnd); err != nil {
			return "", false, err
		}
	}
	data, err := io.ReadAll(f)
	return string(data), truncated, err
}

func sortedStrings(values []string) []string {
	sort.Strings(values)
	return values
}

func schedulerWarning(s *store) string {
	readiness := schedulerReadiness(s)
	if readiness["state"] == "running" {
		return ""
	}
	return fmt.Sprint(readiness["action"])
}
