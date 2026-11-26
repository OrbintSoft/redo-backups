// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestParseAge(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]time.Duration{
		"1m":       time.Minute,
		"90m":      90 * time.Minute,
		"36h":      36 * time.Hour,
		"20d":      20 * day,
		"2w":       14 * day,
		"1d12h":    36 * time.Hour,
		"1w2d3h4m": 9*day + 3*time.Hour + 4*time.Minute,
		"05d":      5 * day,
		"0d1h":     time.Hour, // a zero part is fine while the total is not
	} {
		got, err := ParseAge(value)
		if err != nil {
			t.Errorf("ParseAge(%q): %v", value, err)

			continue
		}

		if got != want {
			t.Errorf("ParseAge(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestParseAgeRejects(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"20",                    // no unit
		"d",                     // no number
		"1d12",                  // trailing number without a unit
		"20x",                   // unknown unit
		"20D",                   // units are lowercase
		"20s",                   // seconds are not a unit of an age
		"1h1d",                  // units out of order
		"1d1d",                  // a unit twice
		"0d",                    // zero
		"0w0d0h0m",              // zero
		"-1d",                   // sign
		"+1d",                   // sign
		"1.5d",                  // fraction
		" 1d",                   // space
		"1d ",                   // space
		"1 d",                   // space
		"99999999999999999999d", // the number overflows
		"300000w",               // the total overflows (over 290 years)
		"15250w15250d",          // the sum overflows
	} {
		if got, err := ParseAge(value); !errors.Is(err, errInvalidAge) {
			t.Errorf("ParseAge(%q) = %v, %v; want errInvalidAge", value, got, err)
		}
	}
}

func TestFormatAge(t *testing.T) {
	t.Parallel()

	for age, want := range map[time.Duration]string{
		0:                "0m",
		59 * time.Second: "0m",
		90 * time.Second: "1m",
		25 * time.Hour:   "1d1h",
		20 * day:         "20d",
		14 * day:         "14d", // days, not weeks
		50*time.Hour + 30*time.Minute + 15*time.Second: "2d2h30m",
		-2 * time.Hour: "-2h",
		math.MinInt64:  "-106751d23h47m",
	} {
		if got := FormatAge(age); got != want {
			t.Errorf("FormatAge(%v) = %q, want %q", age, got, want)
		}
	}
}

func TestFormatAgeRoundTrips(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"1m", "36h", "20d", "1d12h", "3d4h5m"} {
		age, err := ParseAge(value)
		if err != nil {
			t.Fatalf("ParseAge(%q): %v", value, err)
		}

		back, err := ParseAge(FormatAge(age))
		if err != nil || back != age {
			t.Errorf("%q: FormatAge gave %q, which reads back as %v (%v)", value, FormatAge(age), back, err)
		}
	}
}
