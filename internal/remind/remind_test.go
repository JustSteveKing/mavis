package remind

import (
	"strings"
	"testing"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/store"
)

func overdue() store.Invoice {
	return store.Invoice{
		Number: "INV-2026-001", Kind: "invoice", Status: "issued", Currency: "GBP",
		Issued: "2026-08-01", Due: "2026-08-31", Total: 120000, Balance: 120000,
	}
}

func TestFirstReminderIsFriendly(t *testing.T) {
	r, err := Write(Input{Invoice: overdue(), Contact: "Jo Bloggs", Signoff: "Steve", Today: "2026-09-12"})
	if err != nil {
		t.Fatal(err)
	}
	want := `Hi Jo,

A quick reminder that invoice INV-2026-001 for £1,200.00, issued on 1 August 2026, was due on 31 August 2026 and is now 12 days overdue.

If it is already on its way, thank you, and please ignore this. If not, could you let me know when to expect it?

Thanks,
Steve`
	if r.Stage != 1 || r.Subject != "Invoice INV-2026-001: a reminder" || r.Body != want {
		t.Fatalf("got stage %d, %q\n%s", r.Stage, r.Subject, r.Body)
	}
}

func TestSecondPointsBackAtTheFirst(t *testing.T) {
	r, _ := Write(Input{Invoice: overdue(), Signoff: "Steve", Earlier: []string{"2026-09-12"}, Today: "2026-09-26"})
	if r.Stage != 2 || !strings.HasPrefix(r.Body, "Hello,") || !strings.Contains(r.Body, "I last wrote about it on 12 September 2026.") {
		t.Fatalf("%s", r.Body)
	}
}

func TestFinalSetsADeadlineAndOnlyCitesTheActWhenAskedTo(t *testing.T) {
	in := Input{Invoice: overdue(), Signoff: "Steve", Earlier: []string{"2026-09-12", "2026-09-26"}, Today: "2026-10-10"}
	r, _ := Write(in)
	if r.Stage != 3 || !strings.Contains(r.Body, "within 7 days") || !strings.Contains(r.Body, "12 September 2026 and 26 September 2026") {
		t.Fatalf("%s", r.Body)
	}
	if strings.Contains(r.Body, "Late Payment") || strings.Contains(r.Body, "Thanks,") {
		t.Fatalf("the Act only when asked, and no thanks on a final notice:\n%s", r.Body)
	}
	in.Statutory = true
	r, _ = Write(in)
	if !strings.Contains(r.Body, "fixed compensation of £70.00 under the Late Payment of Commercial Debts (Interest) Act 1998") {
		t.Fatalf("%s", r.Body)
	}

	// A fourth repeats the final.
	in.Earlier = append(in.Earlier, "2026-10-10")
	if r, _ := Write(in); r.Stage != Final {
		t.Fatalf("stage %d", r.Stage)
	}
}

func TestChasesWhatIsLeftAfterCredits(t *testing.T) {
	inv := overdue()
	inv.Credited, inv.Balance = 30000, 90000
	r, _ := Write(Input{Invoice: inv, Signoff: "Steve", Today: "2026-09-12"})
	if !strings.Contains(r.Body, "invoice INV-2026-001, of which £900.00 is still to pay") {
		t.Fatalf("%s", r.Body)
	}
}

func TestRefusesWhatIsNotOverdue(t *testing.T) {
	inv := overdue()
	if _, err := Write(Input{Invoice: inv, Today: "2026-08-31"}); err == nil {
		t.Error("due today is not overdue")
	}
	inv.Status = "paid"
	if _, err := Write(Input{Invoice: inv, Today: "2026-10-01"}); err == nil {
		t.Error("a paid invoice")
	}
}

func TestNext(t *testing.T) {
	inv := overdue()
	if stage, due, _ := Next(inv, nil, "2026-09-01"); stage != 1 || !due {
		t.Error("first is due once overdue")
	}
	if _, due, from := Next(inv, []string{"2026-09-01"}, "2026-09-10"); due || from != "2026-09-15" {
		t.Errorf("second waits %d days: due=%v from=%s", Gap, due, from)
	}
	if stage, due, _ := Next(inv, []string{"2026-09-01"}, "2026-09-15"); stage != 2 || !due {
		t.Error("second is due after the gap")
	}
	inv.Balance = 0
	if _, due, _ := Next(inv, nil, "2026-12-01"); due {
		t.Error("nothing owed, nothing due")
	}
}

func TestCompensationBands(t *testing.T) {
	for debt, want := range map[int64]int64{99999: 4000, 100000: 7000, 999999: 7000, 1000000: 10000} {
		if got := Compensation(mpence(debt)); int64(got) != want {
			t.Errorf("Compensation(%d) = %d, want %d", debt, got, want)
		}
	}
}

func mpence(n int64) money.Pence { return money.Pence(n) }
