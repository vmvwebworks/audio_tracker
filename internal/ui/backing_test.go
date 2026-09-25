package ui

import "testing"

func TestParseTime(t *testing.T) {
	ok := map[string]float64{
		"7.35": 7.35, "7,35": 7.35, "0:07.350": 7.35, "0:07,350": 7.35,
		"1:02.5": 62.5, " 12 ": 12, "0:00:07.5": 7.5, "7.35s": 7.35, "-0.2": -0.2,
	}
	for in, want := range ok {
		got, err := parseTime(in)
		if err != nil || got < want-1e-9 || got > want+1e-9 {
			t.Errorf("parseTime(%q) = %v, %v; quiero %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "0:75", "1.5:10", "1:2:3:4"} {
		if _, err := parseTime(in); err == nil {
			t.Errorf("parseTime(%q) debería fallar", in)
		}
	}
	if s := preciseClock(7.35); s != "0:07.350" {
		t.Errorf("preciseClock = %s", s)
	}
}
