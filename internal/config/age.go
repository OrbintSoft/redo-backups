// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Lengths of the calendar-free units an age is written in. A day is always 24
// hours: ages compare instants, so daylight-saving shifts do not apply.
const (
	day  = 24 * time.Hour
	week = 7 * day
)

// ageUnits lists the units of an age, largest first. Go durations stop at
// hours, while backup ages are mostly days.
var ageUnits = []struct {
	symbol byte
	length time.Duration
}{
	{'w', week},
	{'d', day},
	{'h', time.Hour},
	{'m', time.Minute},
}

// errInvalidAge reports an age ParseAge cannot read; the offending value and
// the reason are added by wrapping it.
var errInvalidAge = errors.New("config: invalid age")

// ParseAge reads an age such as "20d", "2w" or "1d12h": one or more whole
// numbers, each followed by a unit, w (weeks of 7 days), d (days of 24 hours),
// h (hours) or m (minutes, not months). Units go from largest to smallest,
// each at most once, and the age must be greater than zero.
func ParseAge(value string) (time.Duration, error) {
	invalid := func(reason string, args ...any) (time.Duration, error) {
		return 0, fmt.Errorf("%w %q: %s", errInvalidAge, value, fmt.Sprintf(reason, args...))
	}

	if value == "" {
		return invalid("empty")
	}

	var total time.Duration

	next := 0 // the largest unit still allowed, as an index into ageUnits

	for rest := value; rest != ""; {
		digits := len(rest) - len(strings.TrimLeft(rest, "0123456789"))
		if digits == 0 {
			return invalid("want a whole number before each unit")
		}

		if digits == len(rest) {
			return invalid("%s has no unit (w, d, h or m)", rest)
		}

		symbol := rest[digits]

		unit := ageUnitIndex(symbol)
		if unit < 0 {
			return invalid("unknown unit %q (want w, d, h or m)", symbol)
		}

		if unit < next {
			return invalid("units must go from largest to smallest, each at most once")
		}

		count, err := strconv.ParseInt(rest[:digits], 10, 64)
		length := ageUnits[unit].length

		if err != nil || count > (math.MaxInt64-int64(total))/int64(length) {
			return invalid("too long")
		}

		total += time.Duration(count) * length
		next = unit + 1
		rest = rest[digits+1:]
	}

	if total == 0 {
		return invalid("must be greater than zero")
	}

	return total, nil
}

// FormatAge writes an age in the units ParseAge reads, from days down to
// whole minutes: 50h30m15s is "2d2h30m". It leaves weeks out, which read less
// easily than days ("20d", not "2w6d"). An age under a minute is "0m", and a
// negative one, a time still to come, is written with a leading "-".
func FormatAge(age time.Duration) string {
	if age < 0 {
		if age == math.MinInt64 {
			age++ // its magnitude does not fit in a Duration
		}

		return "-" + FormatAge(-age)
	}

	if age < time.Minute {
		return "0m"
	}

	var out strings.Builder

	for _, unit := range ageUnits[1:] { // from days: see above
		if count := age / unit.length; count > 0 {
			out.WriteString(strconv.FormatInt(int64(count), 10))
			out.WriteByte(unit.symbol)
		}

		age %= unit.length
	}

	return out.String()
}

// ageUnitIndex returns the index of the unit written symbol in ageUnits, or -1.
func ageUnitIndex(symbol byte) int {
	for i, unit := range ageUnits {
		if unit.symbol == symbol {
			return i
		}
	}

	return -1
}
