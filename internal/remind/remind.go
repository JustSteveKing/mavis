// Package remind writes payment reminders for overdue invoices.
//
// It drafts; it never sends. The wording escalates with each reminder
// already logged: a friendly nudge, then a follow-up that points back at it,
// then a final request with a deadline. Amounts are the balance still owed,
// so a part-credited invoice is chased for what is left.
package remind

import (
	"fmt"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/store"
)

// Gap is how long to leave between reminders.
const Gap = 14

// Final is the last stage. Reminders after it repeat it.
const Final = 3

// Reminder is a drafted message.
type Reminder struct {
	Stage   int    `json:"stage"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Input is everything the wording needs.
type Input struct {
	Invoice   store.Invoice
	Contact   string   // the client's contact, for the greeting
	Signoff   string   // your name
	Earlier   []string // dates of reminders already sent, oldest first
	Today     string
	Statutory bool // cite the Late Payment Act in the final reminder
}

// Next says which reminder comes next and whether it is due yet: the first
// as soon as the invoice is overdue, then each one Gap days after the last.
func Next(inv store.Invoice, earlier []string, today string) (stage int, due bool, from string) {
	stage = min(len(earlier)+1, Final)
	if inv.Balance <= 0 || inv.Due == "" || inv.Due >= today {
		return stage, false, ""
	}
	if len(earlier) == 0 {
		return stage, true, inv.Due
	}
	last, err := time.Parse("2006-01-02", earlier[len(earlier)-1])
	if err != nil {
		return stage, true, today
	}
	from = last.AddDate(0, 0, Gap).Format("2006-01-02")
	return stage, today >= from, from
}

// Write drafts the next reminder for an overdue invoice.
func Write(in Input) (Reminder, error) {
	inv := in.Invoice
	if inv.Kind != "invoice" || inv.Status != "issued" {
		return Reminder{}, fmt.Errorf("%s is not an unpaid invoice", nameOf(inv))
	}
	if inv.Balance <= 0 {
		return Reminder{}, fmt.Errorf("%s has nothing left to pay", inv.Number)
	}
	if inv.Due == "" || inv.Due >= in.Today {
		return Reminder{}, fmt.Errorf("%s is not overdue: it is due on %s", inv.Number, long(inv.Due))
	}

	stage := min(len(in.Earlier)+1, Final)
	owed := amount(inv.Balance, inv.Currency)
	days := daysBetween(inv.Due, in.Today)
	overdue := fmt.Sprintf("%d days overdue", days)
	if days == 1 {
		overdue = "1 day overdue"
	}
	greeting := "Hi " + firstName(in.Contact) + ","
	if in.Contact == "" {
		greeting = "Hello,"
	}
	what := fmt.Sprintf("invoice %s for %s", inv.Number, owed)
	if inv.Credited > 0 {
		what = fmt.Sprintf("invoice %s, of which %s is still to pay", inv.Number, owed)
	}

	var subject string
	var paras []string
	switch stage {
	case 1:
		subject = "Invoice " + inv.Number + ": a reminder"
		paras = []string{
			fmt.Sprintf("A quick reminder that %s, issued on %s, was due on %s and is now %s.", what, long(inv.Issued), long(inv.Due), overdue),
			"If it is already on its way, thank you, and please ignore this. If not, could you let me know when to expect it?",
		}
	case 2:
		subject = "Invoice " + inv.Number + ": second reminder"
		paras = []string{
			fmt.Sprintf("I'm following up on %s, which was due on %s and is now %s. I last wrote about it on %s.", what, long(inv.Due), overdue, long(in.Earlier[len(in.Earlier)-1])),
			"Could you let me know when it will be paid, or whether there is a problem with it I can help sort out?",
		}
	default:
		subject = "Invoice " + inv.Number + ": final reminder"
		paras = []string{
			fmt.Sprintf("%s is now %s. I have written about it on %s.", capitalise(what), overdue, dates(in.Earlier)),
			"Please arrange payment within 7 days, or get in touch if something is stopping it.",
		}
		if in.Statutory && inv.Currency == "GBP" {
			paras = append(paras, fmt.Sprintf(
				"If it remains unpaid, I am entitled to claim statutory interest and fixed compensation of %s under the Late Payment of Commercial Debts (Interest) Act 1998.",
				amount(Compensation(inv.Balance), "GBP")))
		}
	}

	body := greeting + "\n\n" + strings.Join(paras, "\n\n") + "\n\nThanks,\n" + in.Signoff
	if stage == Final {
		body = greeting + "\n\n" + strings.Join(paras, "\n\n") + "\n\n" + in.Signoff
	}
	return Reminder{Stage: stage, Subject: subject, Body: body}, nil
}

// Compensation is the fixed sum the Late Payment Act allows per invoice:
// £40 on a debt under £1,000, £70 under £10,000, and £100 above.
func Compensation(debt money.Pence) money.Pence {
	switch {
	case debt < 100000:
		return 4000
	case debt < 1000000:
		return 7000
	}
	return 10000
}

func amount(p money.Pence, currency string) string {
	switch currency {
	case "GBP":
		return "£" + p.Display()
	case "EUR":
		return "€" + p.Display()
	case "USD":
		return "$" + p.Display()
	}
	return p.Display() + " " + currency
}

func long(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.Format("2 January 2006")
}

// dates lists dates in prose: "3 March", "3 March and 17 March".
func dates(ds []string) string {
	var out []string
	for _, d := range ds {
		out = append(out, long(d))
	}
	switch len(out) {
	case 0:
		return "several occasions"
	case 1:
		return out[0]
	}
	return strings.Join(out[:len(out)-1], ", ") + " and " + out[len(out)-1]
}

func daysBetween(from, to string) int {
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(t.Sub(f).Hours() / 24)
}

func firstName(contact string) string {
	if f := strings.Fields(contact); len(f) > 0 {
		return f[0]
	}
	return contact
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func nameOf(inv store.Invoice) string {
	if inv.Number != "" {
		return inv.Number
	}
	return inv.Slug
}
