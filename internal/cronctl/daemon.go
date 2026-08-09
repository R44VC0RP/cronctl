package cronctl

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

const misfireGrace = 30 * time.Second

func (s *store) runDaemon() error {
	lock := flock.New(filepath.Join(s.paths.locks, "daemon.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("lock daemon: %w", err)
	}
	if !locked {
		return &cliError{Code: "DAEMON_RUNNING", Message: "cronctl daemon is already running", Exit: 6}
	}
	defer lock.Unlock()

	started := time.Now().UTC()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		now := time.Now()
		heartbeat := Heartbeat{SchemaVersion: schemaVersion, PID: os.Getpid(), Version: version, StartedAt: started, LastTick: now.UTC()}
		if err := atomicJSON(s.paths.heartbeat, heartbeat); err != nil {
			return fmt.Errorf("write heartbeat: %w", err)
		}
		if err := s.processDueJobs(now); err != nil {
			return err
		}
		<-ticker.C
	}
}

func (s *store) processDueJobs(now time.Time) error {
	jobs, err := s.list()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if !job.Enabled {
			s.clearJobState(job.Name)
			continue
		}
		parsed, err := parseSchedule(job.Schedule.Canonical, job.Schedule.Timezone)
		if err != nil {
			continue
		}
		state, exists, err := s.getJobState(job)
		if err != nil {
			return fmt.Errorf("read state for %s: %w", job.Name, err)
		}
		if !exists {
			anchor := job.UpdatedAt
			if anchor.IsZero() {
				anchor = now
			}
			state.NextFireAt = parsed.schedule.Next(anchor.In(parsed.location)).UTC()
		}
		if state.NextFireAt.After(now) {
			if !exists {
				if err := s.putJobState(job, state.NextFireAt); err != nil {
					return err
				}
			}
			continue
		}

		firstDue := state.NextFireAt
		latestDue := firstDue
		missedCount := 0
		next := firstDue
		for !next.After(now) && missedCount < 10_000 {
			latestDue = next
			missedCount++
			next = parsed.schedule.Next(next.In(parsed.location)).UTC()
		}
		if !next.After(now) {
			next = parsed.schedule.Next(now.In(parsed.location)).UTC()
		}
		// Persist the decision before execution so a crash cannot fire it twice.
		if err := s.putJobState(job, next); err != nil {
			return err
		}

		if missedCount == 1 && now.Sub(firstDue) <= misfireGrace {
			go func(job Job, scheduledFor time.Time) { _, _ = s.runScheduled(job, "schedule", &scheduledFor, 0) }(job, firstDue)
			continue
		}
		if job.Catchup == "once" {
			go func(job Job, scheduledFor time.Time, count int) {
				_, _ = s.runScheduled(job, "catchup", &scheduledFor, count)
			}(job, latestDue, missedCount)
			continue
		}
		if err := s.recordMissed(job, latestDue, missedCount); err != nil {
			return fmt.Errorf("record missed run for %s: %w", job.Name, err)
		}
	}
	return nil
}
