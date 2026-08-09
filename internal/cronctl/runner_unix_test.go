//go:build !windows

package cronctl

import (
	"strings"
	"testing"
)

func TestRunCapturesOutputAndExitCode(t *testing.T) {
	s := testStore(t)
	job := sampleJob()
	job.Argv = []string{"sh", "-c", "printf hello; exit 7"}
	if _, err := s.put(job, false); err != nil {
		t.Fatal(err)
	}
	record, err := s.run(job, "test")
	if err != nil {
		t.Fatal(err)
	}
	if record.ExitCode != 7 || record.Status != "failed" || !strings.Contains(record.OutputTail, "hello") {
		t.Fatalf("record = %#v", record)
	}
}

func TestRunTimeout(t *testing.T) {
	s := testStore(t)
	job := sampleJob()
	job.Argv = []string{"sh", "-c", "sleep 10"}
	job.Timeout = "10ms"
	record, err := s.run(job, "test")
	if err != nil {
		t.Fatal(err)
	}
	if record.ExitCode != 124 || record.Status != "timed_out" {
		t.Fatalf("record = %#v", record)
	}
}
