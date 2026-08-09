package cronctl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

const (
	outputTailLimit = 16 * 1024
	outputLogLimit  = 10 * 1024 * 1024
	maxRunFiles     = 20
)

type tailWriter struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

type limitWriter struct {
	w         io.Writer
	remaining int64
}

type synchronizedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *synchronizedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func (w *limitWriter) Write(p []byte) (int, error) {
	originalLength := len(p)
	if w.remaining <= 0 {
		return originalLength, nil
	}
	if int64(len(p)) > w.remaining {
		p = p[:w.remaining]
	}
	written, err := w.w.Write(p)
	w.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	return originalLength, nil
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data = append(w.data, p...)
	if len(w.data) > w.limit {
		w.data = append([]byte(nil), w.data[len(w.data)-w.limit:]...)
	}
	return len(p), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.data)
}

func (s *store) run(job Job, trigger string) (RunRecord, error) {
	return s.runScheduled(job, trigger, nil, 0)
}

func (s *store) runScheduled(job Job, trigger string, scheduledFor *time.Time, missedCount int) (RunRecord, error) {
	started := time.Now().UTC()
	record := RunRecord{
		SchemaVersion: schemaVersion,
		RunID:         newID(),
		JobID:         job.ID,
		JobName:       job.Name,
		StartedAt:     started,
		Trigger:       trigger,
		ScheduledFor:  scheduledFor,
		MissedCount:   missedCount,
	}

	lock := flock.New(filepath.Join(s.paths.locks, "job-"+job.ID+".lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return record, fmt.Errorf("lock job: %w", err)
	}
	if !locked && job.Overlap == "skip" {
		record.FinishedAt = time.Now().UTC()
		record.Status = "skipped"
		record.Error = "another instance is already running"
		_ = s.appendHistory(record)
		return record, &cliError{Code: "JOB_RUNNING", Message: record.Error, Exit: 4}
	}
	if locked {
		defer lock.Unlock()
	}
	var timeoutDuration time.Duration
	if job.Timeout != "" {
		timeoutDuration, err = time.ParseDuration(job.Timeout)
		if err != nil || timeoutDuration <= 0 {
			return record, fmt.Errorf("invalid stored timeout %q", job.Timeout)
		}
	}

	runDir := filepath.Join(s.paths.runs, job.Name)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return record, err
	}
	record.LogPath = filepath.Join(runDir, record.RunID+".log")
	logFile, err := os.OpenFile(record.LogPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return record, err
	}
	defer logFile.Close()

	cmd := exec.Command(job.Argv[0], job.Argv[1:]...)
	cmd.Dir = job.Cwd
	cmd.Env = os.Environ()
	for key, value := range job.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	configureProcess(cmd)
	tail := &tailWriter{limit: outputTailLimit}
	output := &synchronizedWriter{w: io.MultiWriter(&limitWriter{w: logFile, remaining: outputLogLimit}, tail)}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		record.FinishedAt = time.Now().UTC()
		record.DurationMS = record.FinishedAt.Sub(started).Milliseconds()
		record.ExitCode = 127
		record.Status = "failed"
		record.Error = fmt.Sprintf("start %q: %v (daemon PATH=%s; use an absolute path or --env PATH=...)", job.Argv[0], err, os.Getenv("PATH"))
		record.OutputTail = tail.String()
		_ = s.appendHistory(record)
		s.pruneRuns(runDir)
		return record, &cliError{Code: "EXEC_FAILED", Message: record.Error, Exit: 127}
	}

	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	var waitErr error
	timedOut := false
	if job.Timeout == "" {
		waitErr = <-wait
	} else {
		timer := time.NewTimer(timeoutDuration)
		select {
		case waitErr = <-wait:
			timer.Stop()
		case <-timer.C:
			timedOut = true
			terminateProcessTree(cmd)
			waitErr = <-wait
		}
	}

	record.FinishedAt = time.Now().UTC()
	record.DurationMS = record.FinishedAt.Sub(started).Milliseconds()
	record.OutputTail = tail.String()
	if timedOut {
		record.ExitCode = 124
		record.Status = "timed_out"
		record.Error = "job exceeded timeout " + job.Timeout
	} else if waitErr != nil {
		record.ExitCode = 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			record.ExitCode = exitErr.ExitCode()
		}
		record.Status = "failed"
		record.Error = waitErr.Error()
	} else {
		record.Status = "succeeded"
	}
	if err := s.appendHistory(record); err != nil {
		return record, fmt.Errorf("record run history: %w", err)
	}
	s.pruneRuns(runDir)
	return record, nil
}

func (s *store) recordMissed(job Job, scheduledFor time.Time, missedCount int) error {
	now := time.Now().UTC()
	record := RunRecord{
		SchemaVersion: schemaVersion,
		RunID:         newID(),
		JobID:         job.ID,
		JobName:       job.Name,
		StartedAt:     now,
		FinishedAt:    now,
		Status:        "missed",
		Trigger:       "catchup",
		ScheduledFor:  &scheduledFor,
		MissedCount:   missedCount,
		Reason:        "catchup_policy_skip",
	}
	return s.appendHistory(record)
}

func (s *store) pruneRuns(runDir string) {
	entries, err := os.ReadDir(runDir)
	if err != nil || len(entries) <= maxRunFiles {
		return
	}
	for _, entry := range entries[:len(entries)-maxRunFiles] {
		if !entry.IsDir() {
			_ = os.Remove(filepath.Join(runDir, entry.Name()))
		}
	}
}

func processID(cmd *exec.Cmd) string {
	if cmd.Process == nil {
		return ""
	}
	return strconv.Itoa(cmd.Process.Pid)
}
