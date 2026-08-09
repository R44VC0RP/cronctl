package cronctl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExportApplyRoundTripIsUnchanged(t *testing.T) {
	s := testStore(t)
	job := sampleJob()
	if _, err := s.put(job, false); err != nil {
		t.Fatal(err)
	}
	result, err := exportCommand(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := result.data.(manifest)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = applyCommand(s, []string{"-f", path, "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	plan := result.data.(map[string]any)
	unchanged := plan["unchanged"].([]string)
	if len(unchanged) != 1 || unchanged[0] != job.Name {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestApplyRejectsDuplicateNamesBeforeWriting(t *testing.T) {
	s := testStore(t)
	job := manifestFromJob(sampleJob())
	doc := manifest{SchemaVersion: 1, Kind: "cronctl.job_set", Jobs: []manifestJob{job, job}}
	data, _ := json.Marshal(doc)
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := applyCommand(s, []string{"-f", path}); err == nil {
		t.Fatal("expected duplicate-name validation error")
	}
	jobs, err := s.list()
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs changed after invalid apply: %#v, %v", jobs, err)
	}
}

func TestApplyAcceptsJSONOutputEnvelope(t *testing.T) {
	s := testStore(t)
	doc := manifest{SchemaVersion: 1, Kind: "cronctl.job_set", Jobs: []manifestJob{manifestFromJob(sampleJob())}}
	envelope := map[string]any{"schema_version": 1, "data": doc}
	data, _ := json.Marshal(envelope)
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := applyCommand(s, []string{"-f", path, "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	created := result.data.(map[string]any)["created"].([]string)
	if len(created) != 1 || created[0] != "backup" {
		t.Fatalf("created = %#v", created)
	}
}

func TestSelectScheduleFlags(t *testing.T) {
	if got, err := selectSchedule("", "15m", ""); err != nil || got != "every 15m" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := selectSchedule("daily at 09:00", "15m", ""); err == nil {
		t.Fatal("expected mutually exclusive schedule error")
	}
}

func TestGlobalJSONFlagDoesNotConsumeChildArgument(t *testing.T) {
	jsonMode, args := extractJSONMode([]string{"add", "report", "--every", "1h", "--json", "--", "tool", "--json"})
	if !jsonMode {
		t.Fatal("global JSON mode was not detected")
	}
	want := []string{"add", "report", "--every", "1h", "--", "tool", "--json"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v", args)
		}
	}
}
