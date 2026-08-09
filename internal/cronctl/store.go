package cronctl

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type store struct{ paths paths }

func newStore() (*store, error) {
	p, err := resolvePaths()
	if err != nil {
		return nil, err
	}
	return &store{paths: p}, nil
}

func (s *store) jobPath(name string) string {
	return filepath.Join(s.paths.jobs, name+".json")
}

func (s *store) jobStatePath(name string) string {
	return filepath.Join(s.paths.jobState, name+".json")
}

func (s *store) get(name string) (Job, error) {
	if !namePattern.MatchString(name) {
		return Job{}, &cliError{Code: "INVALID_NAME", Message: "job names must match [a-z0-9][a-z0-9-]{0,62}", Exit: 5}
	}
	data, err := os.ReadFile(s.jobPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return Job{}, &cliError{Code: "JOB_NOT_FOUND", Message: fmt.Sprintf("job %q not found", name), Exit: 3}
	}
	if err != nil {
		return Job{}, fmt.Errorf("read job: %w", err)
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, fmt.Errorf("decode job %q: %w", name, err)
	}
	if job.SchemaVersion > schemaVersion {
		return Job{}, fmt.Errorf("job %q uses unsupported schema version %d", name, job.SchemaVersion)
	}
	normalizeJob(&job)
	return job, nil
}

func normalizeJob(job *Job) {
	if job.Schedule.Kind == "" {
		if strings.HasPrefix(strings.ToLower(job.Schedule.Canonical), "@every ") {
			job.Schedule.Kind = "every"
		} else {
			job.Schedule.Kind = "cron"
		}
	}
	if job.Catchup == "" {
		job.Catchup = "skip"
	}
}

func (s *store) list() ([]Job, error) {
	entries, err := os.ReadDir(s.paths.jobs)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	jobs := make([]Job, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		job, err := s.get(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
	return jobs, nil
}

func (s *store) put(job Job, replace bool) (string, error) {
	normalizeJob(&job)
	lock := flock.New(filepath.Join(s.paths.locks, "store.lock"))
	if err := lock.Lock(); err != nil {
		return "", fmt.Errorf("lock store: %w", err)
	}
	defer lock.Unlock()

	existing, err := s.get(job.Name)
	if err == nil {
		if jobsEquivalent(existing, job) {
			return "unchanged", nil
		}
		if !replace {
			return "", &cliError{Code: "JOB_CONFLICT", Message: fmt.Sprintf("job %q already exists with a different definition; pass --replace", job.Name), Exit: 4}
		}
		job.ID = existing.ID
		job.CreatedAt = existing.CreatedAt
	} else {
		var ce *cliError
		if !errors.As(err, &ce) || ce.Code != "JOB_NOT_FOUND" {
			return "", err
		}
	}
	if err := atomicJSON(s.jobPath(job.Name), job); err != nil {
		return "", err
	}
	if existing.ID != "" {
		return "updated", nil
	}
	return "created", nil
}

func jobsEquivalent(a, b Job) bool {
	normalizeJob(&a)
	normalizeJob(&b)
	a.ID, b.ID = "", ""
	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func (s *store) remove(name string) error {
	if _, err := s.get(name); err != nil {
		return err
	}
	if err := os.Remove(s.jobPath(name)); err != nil {
		return fmt.Errorf("remove job: %w", err)
	}
	s.clearJobState(name)
	return nil
}

func (s *store) setEnabled(name string, enabled bool) (Job, error) {
	job, err := s.get(name)
	if err != nil {
		return Job{}, err
	}
	job.Enabled = enabled
	job.UpdatedAt = time.Now().UTC()
	if err := atomicJSON(s.jobPath(name), job); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *store) appendHistory(record RunRecord) error {
	lock := flock.New(filepath.Join(s.paths.locks, "history-"+record.JobName+".lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()
	f, err := os.OpenFile(filepath.Join(s.paths.history, record.JobName+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(record); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	path := filepath.Join(s.paths.history, record.JobName+".jsonl")
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 2*1024*1024 {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	if len(lines) > 1000 {
		lines = lines[len(lines)-1000:]
	}
	return atomicBytes(path, append(bytes.Join(lines, []byte{'\n'}), '\n'))
}

func (s *store) history(name string, limit int) ([]RunRecord, error) {
	if _, err := s.get(name); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(s.paths.history, name+".jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return []RunRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var records []RunRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var record RunRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(records) > limit {
		records = records[len(records)-limit:]
	}
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	return records, nil
}

func (s *store) allHistory(limit int) ([]RunRecord, error) {
	jobs, err := s.list()
	if err != nil {
		return nil, err
	}
	var records []RunRecord
	for _, job := range jobs {
		jobRecords, err := s.history(job.Name, limit)
		if err != nil {
			return nil, err
		}
		records = append(records, jobRecords...)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].StartedAt.After(records[j].StartedAt) })
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (s *store) getJobState(job Job) (JobState, bool, error) {
	data, err := os.ReadFile(s.jobStatePath(job.Name))
	if errors.Is(err, os.ErrNotExist) {
		return JobState{}, false, nil
	}
	if err != nil {
		return JobState{}, false, err
	}
	var state JobState
	if err := json.Unmarshal(data, &state); err != nil {
		return JobState{}, false, err
	}
	if state.JobID != job.ID || !state.SpecUpdatedAt.Equal(job.UpdatedAt) {
		return JobState{}, false, nil
	}
	return state, true, nil
}

func (s *store) putJobState(job Job, next time.Time) error {
	state := JobState{
		SchemaVersion: schemaVersion,
		JobID:         job.ID,
		SpecUpdatedAt: job.UpdatedAt,
		NextFireAt:    next.UTC(),
	}
	return atomicJSON(s.jobStatePath(job.Name), state)
}

func (s *store) clearJobState(name string) {
	_ = os.Remove(s.jobStatePath(name))
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicBytes(path, data)
}

func atomicBytes(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceFile(tmpName, path)
}

func replaceFile(source, destination string) error {
	var err error
	for attempt := range 5 {
		err = os.Rename(source, destination)
		if err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	return err
}

func newID() string {
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	return fmt.Sprintf("%013d%s", time.Now().UnixMilli(), hex.EncodeToString(random))
}
