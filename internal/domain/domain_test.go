package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func d(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAgeDays(t *testing.T) {
	today := d("2026-10-30")
	cases := []struct {
		received string
		want     int
	}{
		{"2026-10-30", 0},
		{"2026-10-29", 1},
		{"2026-08-01", 90},
		{"2026-07-31", 91},
		{"2025-10-30", 365},
	}
	for _, c := range cases {
		if got := AgeDays(d(c.received), today); got != c.want {
			t.Errorf("AgeDays(%s) = %d, want %d", c.received, got, c.want)
		}
	}
	// Time of day and zone must not matter.
	late := time.Date(2026, 10, 30, 23, 59, 0, 0, time.UTC)
	if got := AgeDays(d("2026-10-29"), late); got != 1 {
		t.Errorf("AgeDays with time of day = %d, want 1", got)
	}
}

func TestIsAgingBoundaries(t *testing.T) {
	cases := []struct {
		age  int
		want bool
	}{{0, false}, {89, false}, {90, false}, {91, true}, {240, true}}
	for _, c := range cases {
		if got := IsAging(c.age, 90); got != c.want {
			t.Errorf("IsAging(%d, 90) = %v, want %v", c.age, got, c.want)
		}
	}
}

func TestAgingCutoffMatchesIsAging(t *testing.T) {
	today := d("2026-10-30")
	cutoff := AgingCutoff(today, 90)
	for age := 0; age < 200; age++ {
		received := today.AddDate(0, 0, -age)
		inQuery := !received.After(cutoff)
		if inQuery != IsAging(AgeDays(received, today), 90) {
			t.Fatalf("age %d: cutoff says %v, IsAging says %v", age, inQuery, !inQuery)
		}
	}
}

func TestParseActionType(t *testing.T) {
	for _, at := range ActionTypes {
		got, err := ParseActionType(string(at))
		if err != nil || got != at {
			t.Errorf("ParseActionType(%s) = %v, %v", at, got, err)
		}
	}
	for _, bad := range []string{"", "price_reduced", "SELL_IT", "PRICE_REDUCED "} {
		if _, err := ParseActionType(bad); !errors.Is(err, ErrValidation) {
			t.Errorf("ParseActionType(%q) err = %v, want ErrValidation", bad, err)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{Date: d("2026-01-02"), ID: uuid.New()}
	got, err := DecodeCursor(c.Encode())
	if err != nil || !got.Date.Equal(c.Date) || got.ID != c.ID {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "!!!", "e30", "eyJkIjoieCJ9"} {
		if _, err := DecodeCursor(bad); !errors.Is(err, ErrValidation) {
			t.Errorf("DecodeCursor(%q) err = %v, want ErrValidation", bad, err)
		}
	}
}
