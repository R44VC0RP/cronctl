package cronctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

type manifest struct {
	SchemaVersion int           `json:"schema_version"`
	Kind          string        `json:"kind"`
	Jobs          []manifestJob `json:"jobs"`
}

type manifestJob struct {
	Name     string            `json:"name"`
	Argv     []string          `json:"argv"`
	Cwd      string            `json:"cwd,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Schedule ScheduleSpec      `json:"schedule"`
	Enabled  bool              `json:"enabled"`
	Overlap  string            `json:"overlap"`
	Catchup  string            `json:"catchup"`
	Timeout  string            `json:"timeout,omitempty"`
}

func exportCommand(s *store, args []string) (commandResult, error) {
	jobs := make([]Job, 0, len(args))
	if len(args) == 0 {
		var err error
		jobs, err = s.list()
		if err != nil {
			return commandResult{}, err
		}
	} else {
		for _, name := range args {
			job, err := s.get(name)
			if err != nil {
				return commandResult{}, err
			}
			jobs = append(jobs, job)
		}
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
	}
	doc := manifest{SchemaVersion: schemaVersion, Kind: "cronctl.job_set", Jobs: make([]manifestJob, 0, len(jobs))}
	for _, job := range jobs {
		doc.Jobs = append(doc.Jobs, manifestFromJob(job))
	}
	return commandResult{data: doc, plain: prettyJSON(doc)}, nil
}

func applyCommand(s *store, args []string) (commandResult, error) {
	fs := newFlagSet("apply")
	file := fs.String("f", "", "manifest file or - for stdin")
	dryRun := fs.Bool("dry-run", false, "validate and plan without writing")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *file == "" {
		return commandResult{}, usageError("usage: cronctl apply -f FILE [--dry-run]")
	}
	reader, closeReader, err := manifestReader(*file)
	if err != nil {
		return commandResult{}, err
	}
	if closeReader != nil {
		defer closeReader()
	}
	doc, err := decodeManifest(reader)
	if err != nil {
		return commandResult{}, validationError(fmt.Errorf("decode manifest: %w", err))
	}
	if doc.SchemaVersion != schemaVersion || doc.Kind != "cronctl.job_set" {
		return commandResult{}, validationError(fmt.Errorf("manifest must have schema_version 1 and kind cronctl.job_set"))
	}
	seen := make(map[string]bool)
	jobs := make([]Job, 0, len(doc.Jobs))
	created, updated, unchanged := []string{}, []string{}, []string{}
	for _, spec := range doc.Jobs {
		if seen[spec.Name] {
			return commandResult{}, validationError(fmt.Errorf("duplicate job %q", spec.Name))
		}
		seen[spec.Name] = true
		job, err := jobFromManifest(spec)
		if err != nil {
			return commandResult{}, validationError(fmt.Errorf("job %q: %w", spec.Name, err))
		}
		jobs = append(jobs, job)
		existing, err := s.get(job.Name)
		if err == nil {
			if jobsEquivalent(existing, job) {
				unchanged = append(unchanged, job.Name)
			} else {
				updated = append(updated, job.Name)
			}
			continue
		}
		var cliErr *cliError
		if !errors.As(err, &cliErr) || cliErr.Code != "JOB_NOT_FOUND" {
			return commandResult{}, err
		}
		created = append(created, job.Name)
	}
	if !*dryRun {
		for _, job := range jobs {
			if _, err := s.put(job, true); err != nil {
				return commandResult{}, err
			}
		}
	}
	data := map[string]any{
		"dry_run":   *dryRun,
		"created":   sortedStrings(created),
		"updated":   sortedStrings(updated),
		"unchanged": sortedStrings(unchanged),
		"scheduler": schedulerReadiness(s),
	}
	return commandResult{data: data, plain: prettyJSON(data)}, nil
}

func decodeManifest(reader io.Reader) (manifest, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return manifest{}, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return manifest{}, err
	}
	if wrapped, exists := envelope["data"]; exists {
		data = wrapped
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var doc manifest
	if err := decoder.Decode(&doc); err != nil {
		return manifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return manifest{}, fmt.Errorf("manifest must contain exactly one JSON document")
	}
	return doc, nil
}

func manifestFromJob(job Job) manifestJob {
	return manifestJob{
		Name: job.Name, Argv: job.Argv, Cwd: job.Cwd, Env: job.Env,
		Schedule: job.Schedule, Enabled: job.Enabled, Overlap: job.Overlap,
		Catchup: job.Catchup, Timeout: job.Timeout,
	}
}

func jobFromManifest(spec manifestJob) (Job, error) {
	if !namePattern.MatchString(spec.Name) {
		return Job{}, fmt.Errorf("invalid name")
	}
	if len(spec.Argv) == 0 {
		return Job{}, fmt.Errorf("argv cannot be empty")
	}
	raw := spec.Schedule.Raw
	if raw == "" {
		raw = spec.Schedule.Canonical
	}
	parsed, err := parseSchedule(raw, spec.Schedule.Timezone)
	if err != nil {
		return Job{}, err
	}
	canonical := parsed.canonical
	kind := parsed.kind
	if spec.Schedule.Canonical != "" {
		canonicalSchedule, err := parseSchedule(spec.Schedule.Canonical, spec.Schedule.Timezone)
		if err != nil {
			return Job{}, fmt.Errorf("invalid canonical schedule: %w", err)
		}
		canonical = canonicalSchedule.canonical
		kind = canonicalSchedule.kind
	}
	if spec.Overlap != "skip" && spec.Overlap != "allow" {
		return Job{}, fmt.Errorf("overlap must be skip or allow")
	}
	if spec.Catchup != "skip" && spec.Catchup != "once" {
		return Job{}, fmt.Errorf("catchup must be skip or once")
	}
	if spec.Timeout != "" {
		duration, err := time.ParseDuration(spec.Timeout)
		if err != nil || duration <= 0 {
			return Job{}, fmt.Errorf("timeout must be a positive duration")
		}
	}
	now := time.Now().UTC()
	return Job{
		SchemaVersion: schemaVersion, ID: newID(), Name: spec.Name, Argv: spec.Argv,
		Cwd: spec.Cwd, Env: spec.Env,
		Schedule: ScheduleSpec{Raw: raw, Kind: kind, Canonical: canonical, Timezone: parsed.location.String()},
		Enabled:  spec.Enabled, Overlap: spec.Overlap, Catchup: spec.Catchup, Timeout: spec.Timeout,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func manifestReader(path string) (io.Reader, func() error, error) {
	if path == "-" {
		return os.Stdin, nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}
