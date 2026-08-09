package cronctl

import (
	"errors"
	"os"
	"testing"
	"time"
)

func testStore(t *testing.T) *store {
	t.Helper()
	t.Setenv("CRONCTL_HOME", t.TempDir())
	s, err := newStore()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sampleJob() Job {
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	return Job{
		SchemaVersion: schemaVersion,
		ID:            "test-id",
		Name:          "backup",
		Argv:          []string{"backup-tool", "run"},
		Schedule:      ScheduleSpec{Raw: "daily at 09:00", Canonical: "0 9 * * *", Timezone: "UTC"},
		Enabled:       true,
		Overlap:       "skip",
		Catchup:       "skip",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func TestStoreLifecycleAndIdempotency(t *testing.T) {
	s := testStore(t)
	job := sampleJob()
	action, err := s.put(job, false)
	if err != nil || action != "created" {
		t.Fatalf("first put = %q, %v", action, err)
	}
	action, err = s.put(job, false)
	if err != nil || action != "unchanged" {
		t.Fatalf("second put = %q, %v", action, err)
	}

	changed := job
	changed.Argv = []string{"different"}
	_, err = s.put(changed, false)
	var cliErr *cliError
	if !errors.As(err, &cliErr) || cliErr.Code != "JOB_CONFLICT" {
		t.Fatalf("conflict = %v", err)
	}
	if _, err := s.put(changed, true); err != nil {
		t.Fatal(err)
	}
	stored, err := s.get(job.Name)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != job.ID || stored.Argv[0] != "different" {
		t.Fatalf("stored = %#v", stored)
	}
	if err := s.remove(job.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.jobPath(job.Name)); !os.IsNotExist(err) {
		t.Fatalf("job still exists: %v", err)
	}
}

func TestHistoryNewestFirst(t *testing.T) {
	s := testStore(t)
	job := sampleJob()
	if _, err := s.put(job, false); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "three"} {
		if err := s.appendHistory(RunRecord{SchemaVersion: schemaVersion, RunID: id, JobName: job.Name}); err != nil {
			t.Fatal(err)
		}
	}
	records, err := s.history(job.Name, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].RunID != "three" || records[1].RunID != "two" {
		t.Fatalf("history = %#v", records)
	}
}
