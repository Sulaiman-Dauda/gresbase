// Package tools/cron provides a standards-compliant cron expression parser and scheduler.
// Supports standard 5-field cron expressions plus @every and @daily shortcuts.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule represents a parsed cron schedule.
type Schedule struct {
	minutes []int
	hours   []int
	dom     []int // day of month
	months  []int
	dow     []int // day of week
	raw     string
}

// Parse parses a cron expression into a Schedule.
// Supports standard 5-field format: minute hour dom month dow
// Also supports @every <duration> and pre-defined shortcuts.
func Parse(expr string) (*Schedule, error) {
	expr = strings.TrimSpace(expr)

	// Handle shortcuts
	switch expr {
	case "@yearly", "@annually":
		expr = "0 0 1 1 *"
	case "@monthly":
		expr = "0 0 1 * *"
	case "@weekly":
		expr = "0 0 * * 0"
	case "@daily", "@midnight":
		expr = "0 0 * * *"
	case "@hourly":
		expr = "0 * * * *"
	}

	// Handle @every <duration>
	if strings.HasPrefix(expr, "@every ") {
		durStr := strings.TrimPrefix(expr, "@every ")
		if _, err := time.ParseDuration(durStr); err != nil {
			return nil, fmt.Errorf("invalid @every duration: %w", err)
		}
		return &Schedule{raw: expr}, nil
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d in %q", len(fields), expr)
	}

	s := &Schedule{raw: expr}

	var err error
	s.minutes, err = parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	s.hours, err = parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	s.dom, err = parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	s.months, err = parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	s.dow, err = parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}

	return s, nil
}

// Next returns the next time the schedule fires after the given time.
func (s *Schedule) Next(after time.Time) time.Time {
	if strings.HasPrefix(s.raw, "@every ") {
		durStr := strings.TrimPrefix(s.raw, "@every ")
		dur, _ := time.ParseDuration(durStr)
		return after.Add(dur)
	}

	// Start from the next minute
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Limit search to avoid infinite loop
	for i := 0; i < 525600; i++ { // max 1 year
		if s.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}

	return after.Add(24 * time.Hour) // fallback
}

// NextN returns the next N scheduled times.
func (s *Schedule) NextN(after time.Time, n int) []time.Time {
	var times []time.Time
	t := after
	for i := 0; i < n; i++ {
		t = s.Next(t)
		times = append(times, t)
	}
	return times
}

// matches checks if a given time matches the schedule.
func (s *Schedule) matches(t time.Time) bool {
	if !contains(s.minutes, t.Minute()) {
		return false
	}
	if !contains(s.hours, t.Hour()) {
		return false
	}
	if !contains(s.dom, t.Day()) {
		return false
	}
	if !contains(s.months, int(t.Month())) {
		return false
	}
	if !contains(s.dow, int(t.Weekday())) {
		return false
	}
	return true
}

// String returns the original cron expression.
func (s *Schedule) String() string {
	return s.raw
}

// parseField parses a single cron field.
func parseField(field string, min, max int) ([]int, error) {
	if field == "*" {
		return rangeSlice(min, max), nil
	}

	var values []int
	parts := strings.Split(field, ",")

	for _, part := range parts {
		if strings.Contains(part, "/") {
			// Step values: */5, 1-30/5
			split := strings.SplitN(part, "/", 2)
			step, err := strconv.Atoi(split[1])
			if err != nil {
				return nil, fmt.Errorf("invalid step: %s", split[1])
			}

			var rangeStart, rangeEnd int
			if split[0] == "*" {
				rangeStart = min
				rangeEnd = max
			} else if strings.Contains(split[0], "-") {
				rangeParts := strings.SplitN(split[0], "-", 2)
				rangeStart, _ = strconv.Atoi(rangeParts[0])
				rangeEnd, _ = strconv.Atoi(rangeParts[1])
			} else {
				rangeStart, _ = strconv.Atoi(split[0])
				rangeEnd = max
			}

			for i := rangeStart; i <= rangeEnd; i += step {
				values = append(values, i)
			}
		} else if strings.Contains(part, "-") {
			// Range: 1-5
			rangeParts := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid range start: %s", rangeParts[0])
			}
			end, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid range end: %s", rangeParts[1])
			}
			for i := start; i <= end; i++ {
				values = append(values, i)
			}
		} else {
			// Single value
			v, err := strconv.Atoi(part)
			if err != nil {
				// Named values for months and days
				v = nameToValue(part)
				if v < 0 {
					return nil, fmt.Errorf("invalid value: %s", part)
				}
			}
			if v < min || v > max {
				return nil, fmt.Errorf("value %d out of range [%d, %d]", v, min, max)
			}
			values = append(values, v)
		}
	}

	return dedup(values), nil
}

func rangeSlice(min, max int) []int {
	s := make([]int, max-min+1)
	for i := range s {
		s[i] = min + i
	}
	return s
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func dedup(s []int) []int {
	seen := make(map[int]bool)
	var result []int
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// nameToValue converts named month/day values to numbers.
func nameToValue(name string) int {
	name = strings.ToLower(name)
	months := map[string]int{
		"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	}
	days := map[string]int{
		"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
	}

	if v, ok := months[name]; ok {
		return v
	}
	if v, ok := days[name]; ok {
		return v
	}
	return -1
}
