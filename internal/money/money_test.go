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

func TestDisplay(t *testing.T) {
	for p, want := range map[Pence]string{125000000: "1,250,000.00", 65000: "650.00", 100000: "1,000.00", -123456: "-1,234.56"} {
		if got := p.Display(); got != want {
			t.Errorf("%d.Display() = %q, want %q", p, got, want)
		}
	}
}

func TestMulDiv(t *testing.T) {
	// 650.00 a day for 9h of a 7.5h day: 780.00
	if got := Pence(65000).MulDiv(540, 450); got != 78000 {
		t.Errorf("got %d", got)
	}
	// 100.00 / 3 rounds to 33.33; 200.00 / 3 rounds to 66.67
	if got := Pence(10000).MulDiv(1, 3); got != 3333 {
		t.Errorf("got %d", got)
	}
	if got := Pence(20000).MulDiv(1, 3); got != 6667 {
		t.Errorf("got %d", got)
	}
}
