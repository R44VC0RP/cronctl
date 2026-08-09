package cronctl

import "time"

var version = "dev"

const schemaVersion = 1

type Job struct {
	SchemaVersion int               `json:"schema_version"`
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Argv          []string          `json:"argv"`
	Cwd           string            `json:"cwd,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Schedule      ScheduleSpec      `json:"schedule"`
	Enabled       bool              `json:"enabled"`
	Overlap       string            `json:"overlap"`
	Catchup       string            `json:"catchup"`
	Timeout       string            `json:"timeout,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type ScheduleSpec struct {
	Raw       string `json:"raw"`
	Kind      string `json:"kind"`
	Canonical string `json:"canonical"`
	Timezone  string `json:"timezone"`
}

type RunRecord struct {
	SchemaVersion int        `json:"schema_version"`
	RunID         string     `json:"run_id"`
	JobID         string     `json:"job_id"`
	JobName       string     `json:"job_name"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    time.Time  `json:"finished_at"`
	DurationMS    int64      `json:"duration_ms"`
	ExitCode      int        `json:"exit_code"`
	Status        string     `json:"status"`
	Trigger       string     `json:"trigger"`
	ScheduledFor  *time.Time `json:"scheduled_for,omitempty"`
	MissedCount   int        `json:"missed_count,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	LogPath       string     `json:"log_path"`
	OutputTail    string     `json:"output_tail,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type JobState struct {
	SchemaVersion int       `json:"schema_version"`
	JobID         string    `json:"job_id"`
	SpecUpdatedAt time.Time `json:"spec_updated_at"`
	NextFireAt    time.Time `json:"next_fire_at"`
}

type Heartbeat struct {
	SchemaVersion int       `json:"schema_version"`
	PID           int       `json:"pid"`
	Version       string    `json:"version"`
	StartedAt     time.Time `json:"started_at"`
	LastTick      time.Time `json:"last_tick"`
}

type cliError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Exit    int    `json:"-"`
}

func (e *cliError) Error() string { return e.Message }
