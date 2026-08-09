package cronctl

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"

	cronlib "github.com/robfig/cron/v3"
)

var (
	dailyPattern  = regexp.MustCompile(`(?i)^daily\s+at\s+([01]?\d|2[0-3]):([0-5]\d)$`)
	weeklyPattern = regexp.MustCompile(`(?i)^weekly\s+on\s+(sun|mon|tue|wed|thu|fri|sat)\s+at\s+([01]?\d|2[0-3]):([0-5]\d)$`)
	everyPattern  = regexp.MustCompile(`(?i)^every\s+(.+)$`)
	cronParser    = cronlib.NewParser(cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow | cronlib.Descriptor)
)

type parsedSchedule struct {
	kind      string
	canonical string
	schedule  cronlib.Schedule
	location  *time.Location
}

func parseSchedule(raw, timezone string) (parsedSchedule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedSchedule{}, fmt.Errorf("schedule cannot be empty")
	}
	if timezone == "" || strings.EqualFold(timezone, "local") {
		timezone = "Local"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return parsedSchedule{}, fmt.Errorf("unknown timezone %q", timezone)
	}

	canonical := raw
	kind := "cron"
	if match := dailyPattern.FindStringSubmatch(raw); match != nil {
		canonical = fmt.Sprintf("%s %s * * *", match[2], match[1])
	} else if match := weeklyPattern.FindStringSubmatch(raw); match != nil {
		canonical = fmt.Sprintf("%s %s * * %s", match[3], match[2], strings.ToUpper(match[1]))
	} else if match := everyPattern.FindStringSubmatch(raw); match != nil {
		if _, err := time.ParseDuration(match[1]); err != nil {
			return parsedSchedule{}, fmt.Errorf("invalid interval %q: %w", match[1], err)
		}
		canonical = "@every " + match[1]
		kind = "every"
	} else if strings.HasPrefix(strings.ToLower(raw), "@every ") {
		kind = "every"
	}

	schedule, err := cronParser.Parse(canonical)
	if err != nil {
		return parsedSchedule{}, fmt.Errorf("invalid schedule %q: %w", raw, err)
	}
	return parsedSchedule{kind: kind, canonical: canonical, schedule: schedule, location: location}, nil
}

func nextTimes(spec ScheduleSpec, after time.Time, count int) ([]time.Time, error) {
	parsed, err := parseSchedule(spec.Canonical, spec.Timezone)
	if err != nil {
		return nil, err
	}
	next := make([]time.Time, 0, count)
	cursor := after.In(parsed.location)
	for range count {
		cursor = parsed.schedule.Next(cursor)
		next = append(next, cursor)
	}
	return next, nil
}
