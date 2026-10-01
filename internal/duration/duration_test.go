package duration

import "testing"

func TestParse(t *testing.T) {
	for in, want := range map[string]int{
		"1d":    450,
		"0.5d":  225,
		"3h":    180,
		"1.5h":  90,
		"45m":   45,
		"1h30m": 90,
		"1d 2h": 570,
		"2H":    120,
	} {
		got, err := Parse(in, DefaultDay)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "3", "three hours", "1h1h", "h", "1x", "0h", "-1h", "1h junk"} {
		if _, err := Parse(bad, DefaultDay); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestDayLengthMatters(t *testing.T) {
	if got, _ := Parse("1d", 480); got != 480 {
		t.Fatalf("an 8 hour day: %d", got)
	}
}

func TestRender(t *testing.T) {
	for m, want := range map[int]string{450: "7h30m", 180: "3h", 45: "45m", 61: "1h01m"} {
		if got := Hours(m); got != want {
			t.Errorf("Hours(%d) = %q, want %q", m, got, want)
		}
	}
	for m, want := range map[int]string{450: "1d", 225: "0.5d", 960: "2.13d", 900: "2d"} {
		if got := Days(m, DefaultDay); got != want {
			t.Errorf("Days(%d) = %q, want %q", m, got, want)
		}
	}
}
