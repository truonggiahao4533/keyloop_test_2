package domain

import (
	"testing"
	"time"
)

// TestSystemClockUsesDealershipDate checks that today is the date in the
// dealership's zone, whatever zone the server runs in. The two offsets are 25
// hours apart, so the server and the dealership are always on different dates.
func TestSystemClockUsesDealershipDate(t *testing.T) {
	orig := time.Local
	t.Cleanup(func() { time.Local = orig })
	cases := []struct{ dealership, server int }{{14, -11}, {-11, 14}}
	for _, c := range cases {
		time.Local = time.FixedZone("server", c.server*3600)
		clock := SystemClock{Location: time.FixedZone("dealership", c.dealership*3600)}
		shift := time.Duration(c.dealership) * time.Hour
		before := DateOf(time.Now().UTC().Add(shift))
		got := clock.Today()
		after := DateOf(time.Now().UTC().Add(shift))
		// Comparing against both sides keeps this stable across midnight.
		if !got.Equal(before) && !got.Equal(after) {
			t.Errorf("dealership UTC%+d: Today() = %s, want %s", c.dealership, got.Format(time.DateOnly), before.Format(time.DateOnly))
		}
	}
}
