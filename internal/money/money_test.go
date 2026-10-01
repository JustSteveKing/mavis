package money

import "testing"

func TestParse(t *testing.T) {
	for in, want := range map[string]Pence{
		"650":      65000,
		"650.5":    65050,
		"650.50":   65050,
		"1,250.00": 125000,
		".99":      99,
		"-12.30":   -1230,
	} {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "1.234", "12.", "£650"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestString(t *testing.T) {
	for p, want := range map[Pence]string{65000: "650.00", 5: "0.05", -1230: "-12.30", 0: "0.00"} {
		if got := p.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", p, got, want)
		}
	}
}
