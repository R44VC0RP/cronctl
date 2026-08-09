package cronctl

import (
	"testing"
	"time"
)

func TestParseScheduleAliases(t *testing.T) {
	tests := []struct {
		raw       string
		canonical string
	}{
		{"every 15m", "@every 15m"},
		{"daily at 09:30", "30 09 * * *"},
		{"weekly on mon at 7:05", "05 7 * * MON"},
		{"0 */6 * * *", "0 */6 * * *"},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			parsed, err := parseSchedule(test.raw, "UTC")
			if err != nil {
				t.Fatal(err)
			}
			if parsed.canonical != test.canonical {
				t.Fatalf("canonical = %q, want %q", parsed.canonical, test.canonical)
			}
		})
	}
}

func TestNextTimesUsesTimezone(t *testing.T) {
	spec := ScheduleSpec{Raw: "daily at 09:00", Canonical: "0 9 * * *", Timezone: "America/New_York"}
	after := time.Date(2026, time.January, 2, 13, 0, 0, 0, time.UTC)
	next, err := nextTimes(spec, after, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := next[0].UTC(); !got.Equal(time.Date(2026, time.January, 2, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("next = %s", got)
	}
}

func TestInvalidSchedule(t *testing.T) {
	if _, err := parseSchedule("whenever convenient", "UTC"); err == nil {
		t.Fatal("expected invalid schedule error")
	}
	if _, err := parseSchedule("daily at 09:00", "Not/AZone"); err == nil {
		t.Fatal("expected invalid timezone error")
	}
}
