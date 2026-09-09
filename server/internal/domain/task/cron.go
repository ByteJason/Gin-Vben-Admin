package task

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidCronExpression = errors.New("invalid cron expression")

type cronField struct {
	values   map[int]bool
	wildcard bool
}
type cronSpec struct {
	fields  [7]cronField
	seconds bool
}

func parseCron(expression string) (cronSpec, error) {
	expression = strings.TrimSpace(expression)
	aliases := map[string]string{"@hourly": "0 * * * *", "@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@weekly": "0 0 * * 0", "@monthly": "0 0 1 * *", "@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *"}
	if alias, ok := aliases[expression]; ok {
		expression = alias
	}
	parts := strings.Fields(expression)
	spec := cronSpec{seconds: len(parts) >= 6}
	if len(parts) == 5 {
		parts = append([]string{"0"}, parts...)
	}
	if len(parts) == 6 {
		parts = append(parts, "*")
	}
	if len(parts) != 7 {
		return spec, ErrInvalidCronExpression
	}
	mins := []int{0, 0, 0, 1, 1, 0, 1970}
	maxs := []int{59, 59, 23, 31, 12, 7, 2199}
	for i, part := range parts {
		f := cronField{values: map[int]bool{}, wildcard: part == "*" || part == "?"}
		for _, term := range strings.Split(part, ",") {
			if term == "" {
				return spec, ErrInvalidCronExpression
			}
			step := 1
			span := strings.Split(term, "/")
			if len(span) > 2 {
				return spec, ErrInvalidCronExpression
			}
			if len(span) == 2 {
				n, e := strconv.Atoi(span[1])
				if e != nil || n < 1 || n > maxs[i]-mins[i]+1 {
					return spec, ErrInvalidCronExpression
				}
				step = n
			}
			base := span[0]
			a, b := mins[i], maxs[i]
			if base != "*" && base != "?" {
				ends := strings.Split(base, "-")
				if len(ends) > 2 {
					return spec, ErrInvalidCronExpression
				}
				var e error
				a, e = strconv.Atoi(ends[0])
				if e != nil {
					return spec, ErrInvalidCronExpression
				}
				b = a
				if len(ends) == 2 {
					b, e = strconv.Atoi(ends[1])
					if e != nil {
						return spec, ErrInvalidCronExpression
					}
				} else if len(span) == 2 {
					b = maxs[i]
				}
			}
			if a < mins[i] || b > maxs[i] || a > b {
				return spec, ErrInvalidCronExpression
			}
			for value := a; value <= b; value += step {
				f.values[value] = true
			}
		}
		spec.fields[i] = f
	}
	// A restricted day-of-month with a restricted month and an unrestricted
	// weekday must describe at least one real calendar date.  Rejecting an
	// expression such as "0 0 31 2 *" at validation time prevents definitions
	// that can never produce a run while preserving standard DOM/DOW OR rules.
	if !spec.fields[3].wildcard && !spec.fields[4].wildcard && spec.fields[5].wildcard && !hasCalendarDate(spec) {
		return spec, ErrInvalidCronExpression
	}
	return spec, nil
}

func hasCalendarDate(spec cronSpec) bool {
	// Day/month validity only depends on leap-year status, so four consecutive
	// years (including 2000, a leap year) cover every calendar shape. Use the
	// explicit year set when present.
	years := make([]int, 0, len(spec.fields[6].values))
	if !spec.fields[6].wildcard {
		for year := range spec.fields[6].values {
			years = append(years, year)
		}
	} else {
		for year := 2000; year < 2004; year++ {
			years = append(years, year)
		}
	}
	for _, year := range years {
		for month := range spec.fields[4].values {
			for day := range spec.fields[3].values {
				date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
				if int(date.Month()) == month && date.Day() == day && date.Year() == year {
					return true
				}
			}
		}
	}
	return false
}
func ValidateCron(expression string) error { _, err := parseCron(expression); return err }
func HasSeconds(expression string) bool {
	s, err := parseCron(expression)
	return err == nil && s.seconds
}
func (s cronSpec) dayMatches(at time.Time) bool {
	day := s.fields[3].values[at.Day()]
	weekday := s.fields[5].values[int(at.Weekday())] || (at.Weekday() == time.Sunday && s.fields[5].values[7])
	if !s.fields[3].wildcard && !s.fields[5].wildcard {
		return day || weekday
	}
	return day && weekday
}
func MatchesCron(expression string, at time.Time) bool {
	s, err := parseCron(expression)
	return err == nil && s.fields[6].values[at.Year()] && s.fields[4].values[int(at.Month())] && s.dayMatches(at) && s.fields[2].values[at.Hour()] && s.fields[1].values[at.Minute()] && s.fields[0].values[at.Second()]
}

// NextExecutions skips calendar units instead of polling each second. A five
// year horizon bounds nonexistent dates and sparse schedules deterministically.
func NextExecutions(expression, timezone string, after time.Time, count int) ([]time.Time, error) {
	if count < 1 || count > 10 {
		return nil, ErrInvalidCronExpression
	}
	s, err := parseCron(expression)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, err
	}
	cursor := after.In(loc).Truncate(time.Second).Add(time.Second)
	limit := after.In(loc).AddDate(5, 0, 1)
	out := make([]time.Time, 0, count)
	for cursor.Before(limit) && len(out) < count {
		y, m, d := cursor.Date()
		if !s.fields[6].values[y] {
			cursor = time.Date(y+1, 1, 1, 0, 0, 0, 0, loc)
			continue
		}
		if !s.fields[4].values[int(m)] {
			cursor = time.Date(y, m+1, 1, 0, 0, 0, 0, loc)
			continue
		}
		if !s.dayMatches(cursor) {
			cursor = time.Date(y, m, d+1, 0, 0, 0, 0, loc)
			continue
		}
		if !s.fields[2].values[cursor.Hour()] {
			cursor = cursor.Truncate(time.Hour).Add(time.Hour)
			continue
		}
		if !s.fields[1].values[cursor.Minute()] {
			cursor = cursor.Truncate(time.Minute).Add(time.Minute)
			continue
		}
		if !s.fields[0].values[cursor.Second()] {
			cursor = cursor.Add(time.Second)
			continue
		}
		out = append(out, cursor)
		cursor = cursor.Add(time.Second)
	}
	if len(out) < count {
		return nil, ErrInvalidCronExpression
	}
	return out, nil
}
