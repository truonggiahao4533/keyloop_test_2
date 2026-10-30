package domain

import "time"

// Clock supplies "today". It is injected so tests can control the date.
type Clock interface{ Today() time.Time }

// SystemClock returns the current UTC date.
type SystemClock struct{}

// Today returns the current date at UTC midnight.
func (SystemClock) Today() time.Time { return DateOf(time.Now()) }

// FixedClock always returns the same date. Useful in tests and scripts.
type FixedClock time.Time

// Today returns the fixed date at UTC midnight.
func (f FixedClock) Today() time.Time { return DateOf(time.Time(f)) }

// DateOf truncates t to its calendar date at UTC midnight.
func DateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
