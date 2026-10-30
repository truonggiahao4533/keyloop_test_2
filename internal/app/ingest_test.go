package app

import "testing"

func TestParsePriceCents(t *testing.T) {
	good := map[string]int64{"0": 0, "1": 100, "24999": 2499900, "24999.5": 2499950, "24999.99": 2499999, "9999999999.99": 999999999999}
	for in, want := range good {
		if got, err := parsePriceCents(in); err != nil || got != want {
			t.Errorf("parsePriceCents(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-1", "1.234", "1e5", ".5", "10000000000", "1,000", "12.a"} {
		if _, err := parsePriceCents(in); err == nil {
			t.Errorf("parsePriceCents(%q) succeeded", in)
		}
	}
}

func TestValidVIN(t *testing.T) {
	for _, v := range []string{"1HGCM82633A004352", "BNDRY000000000090"} {
		if !validVIN(v) {
			t.Errorf("%s should be valid", v)
		}
	}
	for _, v := range []string{"", "1HGCM82633A00435", "1HGCM82633A0043520", "1HGCM82633A00435I", "1hgcm82633a004352"} {
		if validVIN(v) {
			t.Errorf("%s should be invalid", v)
		}
	}
}
