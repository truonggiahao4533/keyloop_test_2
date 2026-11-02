package domain

import "time"

// Clock supplies "today". It is injected so tests can control the date.
type Clock interface{ Today() time.Time }

// SystemClock returns the current date in the dealership's time zone.
type SystemClock struct{ Location *time.Location }

// Today returns the current date in Location, at UTC midnight.
func (c SystemClock) Today() time.Time { return DateOf(time.Now().In(c.Location)) }

// FixedClock always returns the same date. Useful in tests and scripts.
type FixedClock time.Time

// Today returns the fixed date at UTC midnight.
func (f FixedClock) Today() time.Time { return DateOf(time.Time(f)) }

// DateOf truncates t to its calendar date at UTC midnight.
func DateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
