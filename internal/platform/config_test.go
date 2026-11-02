package platform

import "testing"

// TestDealershipLocation checks the default, a valid zone, and the values that
// must stop the service from starting.
func TestDealershipLocation(t *testing.T) {
	cases := []struct {
		value   string
		want    string
		wantErr bool
	}{
		{value: "", want: "UTC"},
		{value: "Asia/Ho_Chi_Minh", want: "Asia/Ho_Chi_Minh"},
		{value: "Mars/Olympus", wantErr: true},
		{value: "Local", wantErr: true},
	}
	for _, c := range cases {
		t.Setenv("DEALERSHIP_TIMEZONE", c.value)
		loc, err := DealershipLocation()
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: got %v, want an error", c.value, loc)
			}
			continue
		}
		if err != nil || loc.String() != c.want {
			t.Errorf("%q: got %v, %v, want %s", c.value, loc, err, c.want)
		}
	}
}
