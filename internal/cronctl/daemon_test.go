package cronctl

import (
	"testing"
	"time"
)

func TestMissedRunsAreRecordedAndNotRepeated(t *testing.T) {
	s := testStore(t)
	job := sampleJob()
	job.Schedule = ScheduleSpec{Raw: "every 1m", Kind: "every", Canonical: "@every 1m", Timezone: "UTC"}
	job.Catchup = "skip"
	if _, err := s.put(job, false); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.January, 2, 4, 0, 0, 0, time.UTC)
	if err := s.putJobState(job, now.Add(-3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.processDueJobs(now); err != nil {
		t.Fatal(err)
	}
	records, err := s.history(job.Name, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != "missed" || records[0].MissedCount != 4 {
		t.Fatalf("records = %#v", records)
	}
	if err := s.processDueJobs(now); err != nil {
		t.Fatal(err)
	}
	records, _ = s.history(job.Name, 10)
	if len(records) != 1 {
		t.Fatalf("missed decision repeated: %#v", records)
	}
}

func TestCatchupOnceRunsOneCommand(t *testing.T) {
	s := testStore(t)
	job := sampleJob()
	job.Argv = []string{"go", "version"}
	job.Schedule = ScheduleSpec{Raw: "every 1m", Kind: "every", Canonical: "@every 1m", Timezone: "UTC"}
	job.Catchup = "once"
	if _, err := s.put(job, false); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.January, 2, 4, 0, 0, 0, time.UTC)
	if err := s.putJobState(job, now.Add(-3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.processDueJobs(now); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		records, _ := s.history(job.Name, 10)
		if len(records) == 1 {
			if records[0].Trigger != "catchup" || records[0].MissedCount != 4 || records[0].Status != "succeeded" {
				t.Fatalf("record = %#v", records[0])
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("catchup run was not recorded")
}
