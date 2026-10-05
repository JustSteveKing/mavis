package cmd

import (
	"strings"
	"testing"
)

func TestDeliveredOnItemWork(t *testing.T) {
	setup(t)
	for _, args := range [][]string{
		{"client", "add", "sevalla", "--name", "Sevalla"},
		{"engagement", "add", "sevalla", "articles", "--basis", "item", "--rate", "750", "--unit", "article"},
		{"engagement", "add", "sevalla", "consulting", "--basis", "day", "--rate", "600"},
	} {
		if _, err := run(t, args...); err != nil {
			t.Fatal(args, err)
		}
	}
	out, err := run(t, "delivered", "sevalla-articles", "Queues", "deep", "dive", "--date", "2026-09-10")
	if err != nil || !strings.Contains(out, "Recorded 1 article on 2026-09-10 to sevalla-articles") {
		t.Fatalf("%q %v", out, err)
	}
	if out, err = run(t, "delivered", "sevalla-articles", "Two tips", "--items", "2", "--date", "2026-09-11"); err != nil || !strings.Contains(out, "Recorded 2 articles") {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := run(t, "delivered", "sevalla-consulting"); err == nil || !strings.Contains(err.Error(), "not item work") {
		t.Fatalf("day work took a delivery: %v", err)
	}

	out, _ = run(t, "engagement", "show", "sevalla-articles")
	if !strings.Contains(out, "item @ 750.00 per article") {
		t.Errorf("show: %s", out)
	}
	out, _ = run(t, "time", "list", "--month", "2026-09")
	if !strings.Contains(out, "ITEMS") || !strings.Contains(out, "Queues deep dive") {
		t.Errorf("time list: %s", out)
	}
	out, _ = run(t, "stats", "--month", "2026-09")
	if !strings.Contains(out, "DELIVERED") || !strings.Contains(out, "3 articles") || !strings.Contains(out, "2,250.00") {
		t.Errorf("stats: %s", out)
	}
}
