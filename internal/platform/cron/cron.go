// Package cron preserves the five-field schedule subset used by directory sync.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func Matches(expression string, at time.Time) bool {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return false
	}
	values := []int{at.Minute(), at.Hour(), at.Day(), int(at.Month()), int(at.Weekday())}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i := range fields {
		if !cronFieldMatches(fields[i], values[i], limits[i][0], limits[i][1]) {
			return false
		}
	}
	return true
}

func cronFieldMatches(field string, value, min, max int) bool {
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "*" {
			return true
		}
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err == nil && step > 0 && value%step == 0 {
				return true
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err == nil && n >= min && n <= max && n == value {
			return true
		}
	}
	return false
}

func Validate(expression string) error {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return fmt.Errorf("schedule must be a five-field cron expression")
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, field := range fields {
		if field == "*" {
			continue
		}
		if strings.HasPrefix(field, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(field, "*/"))
			if err != nil || step < 1 {
				return fmt.Errorf("invalid cron field %q", field)
			}
			continue
		}
		valid := true
		for _, part := range strings.Split(field, ",") {
			n, err := strconv.Atoi(part)
			if err != nil || n < limits[i][0] || n > limits[i][1] {
				valid = false
			}
		}
		if !valid {
			return fmt.Errorf("invalid cron field %q", field)
		}
	}
	return nil
}
