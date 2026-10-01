// Package duration reads and renders time worked: 1d, 0.5d, 3h, 45m, 1h30m.
//
// Days depend on how long a working day is, so everything is converted to
// whole minutes against a day length the caller supplies. What a person typed
// is kept alongside, so a timesheet still says 1d, not 450m.
package duration

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// DefaultDay is 7.5 hours, the usual UK contract day.
const DefaultDay = 450

var part = regexp.MustCompile(`(\d+(?:\.\d+)?)([dhm])`)

// Parse reads a duration into minutes, given the minutes in a working day.
// Parts may combine (1h30m) but each unit appears once at most.
func Parse(s string, dayMinutes int) (int, error) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	matches := part.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return 0, fmt.Errorf("%q: use 1d, 0.5d, 3h, 45m or 1h30m", s)
	}

	pos := 0
	seen := map[string]bool{}
	var total float64
	for _, m := range matches {
		if m[0] != pos {
			return 0, fmt.Errorf("%q: use 1d, 0.5d, 3h, 45m or 1h30m", s)
		}
		pos = m[1]
		n, _ := strconv.ParseFloat(s[m[2]:m[3]], 64)
		unit := s[m[4]:m[5]]
		if seen[unit] {
			return 0, fmt.Errorf("%q: %s given twice", s, unit)
		}
		seen[unit] = true
		switch unit {
		case "d":
			total += n * float64(dayMinutes)
		case "h":
			total += n * 60
		case "m":
			total += n
		}
	}
	if pos != len(s) {
		return 0, fmt.Errorf("%q: use 1d, 0.5d, 3h, 45m or 1h30m", s)
	}
	minutes := int(math.Round(total))
	if minutes <= 0 {
		return 0, fmt.Errorf("%q is no time at all", s)
	}
	return minutes, nil
}

// Hours renders minutes as 7h30m, 3h or 45m.
func Hours(minutes int) string {
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%02dm", h, m)
}

// Days renders minutes as days to two decimal places, trimmed: 1d, 0.5d,
// 2.13d.
func Days(minutes, dayMinutes int) string {
	d := strconv.FormatFloat(float64(minutes)/float64(dayMinutes), 'f', 2, 64)
	d = strings.TrimRight(strings.TrimRight(d, "0"), ".")
	return d + "d"
}
