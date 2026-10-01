package cmd

import (
	"errors"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/JustSteveKing/mavis/internal/duration"
	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

func newStatsCommand(a *app) *cobra.Command {
	var month, year, from, to string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Time and its value, by engagement, for a period",
		Long: `Time logged in a period and what it is worth at your rates:

  day       days logged times the rate
  hourly    hours logged times the rate
  retainer  the monthly rate for each month of the period it was running
  fixed     not valued per period; shown to date, as budget per day logged

This is value, not revenue: what invoices say is a separate question.
PER DAY is value over days logged. On the total line it counts only
engagements with both a value and time, so unpriced time does not drag it
down and a retainer with nothing logged does not inflate it.

Amounts in different currencies are totalled separately.`,
		Example: `  mavis stats
  mavis stats --month 2026-09
  mavis stats --year 2026
  mavis stats --from 2026-04-06 --to 2027-04-05`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			p, label, err := period(s, month, year, from, to)
			if err != nil {
				return err
			}
			st, problems, err := s.Stats(p)
			if err != nil {
				return err
			}
			a.warn(problems)
			if a.jsonOut {
				return a.emitJSON(st)
			}

			a.printf("%s\n\n", label)
			if len(st.Engagements) == 0 && len(st.Fixed) == 0 {
				a.printf("No time logged.\n")
				return nil
			}

			day := s.DayMinutes
			if len(st.Engagements) > 0 {
				rows := [][]string{{"CLIENT", "ENGAGEMENT", "BASIS", "TIME", "VALUE", "PER DAY"}}
				for _, e := range st.Engagements {
					basis := e.Basis
					if r, err := money.Parse(e.Rate); err == nil && e.Basis != "fixed" {
						basis += " @ " + r.Display()
					}
					rows = append(rows, []string{e.Client, e.Title, basis, timeCell(e.Minutes, day), amount(e.Value), amount(e.PerDay)})
				}
				for _, t := range st.Totals {
					label := "Total"
					if len(st.Totals) > 1 || t.Currency != "GBP" {
						label += " " + t.Currency
					}
					v := t.Value
					rows = append(rows, []string{label, "", "", timeCell(t.Minutes, day), amount(&v), amount(t.PerDay)})
				}
				table(a.out, "", map[int]bool{3: true, 4: true, 5: true}, rows)
			}

			if len(st.Fixed) > 0 {
				a.printf("\nFixed price, to date\n")
				w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
				for _, f := range st.Fixed {
					rate := "nothing logged yet"
					if f.PerDay != nil {
						rate = f.PerDay.Display() + " a day"
					}
					fmt.Fprintf(w, "  %s\t%s\tbudget %s\t%s logged\t%s\n", f.Client, f.Title, f.Budget.Display(), duration.Days(f.Minutes, day), rate)
				}
				w.Flush()
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&month, "month", "", "YYYY-MM (the default is this month)")
	f.StringVar(&year, "year", "", "YYYY")
	f.StringVar(&from, "from", "", "start of a custom period, YYYY-MM-DD")
	f.StringVar(&to, "to", "", "end of a custom period, YYYY-MM-DD (default: today)")
	return cmd
}

func period(s *store.Store, month, year, from, to string) (store.Period, string, error) {
	given := 0
	for _, v := range []string{month, year, from} {
		if v != "" {
			given++
		}
	}
	if given > 1 {
		return store.Period{}, "", errors.New("give one of --month, --year or --from")
	}
	if to != "" && from == "" {
		return store.Period{}, "", errors.New("--to needs --from")
	}
	switch {
	case year != "":
		p, err := store.YearPeriod(year)
		return p, year, err
	case from != "":
		if to == "" {
			to = s.Now().Format("2006-01-02")
		}
		f, err1 := time.Parse("2006-01-02", from)
		t, err2 := time.Parse("2006-01-02", to)
		if err1 != nil || err2 != nil {
			return store.Period{}, "", errors.New("--from and --to are YYYY-MM-DD")
		}
		if t.Before(f) {
			return store.Period{}, "", errors.New("--to is before --from")
		}
		return store.Period{From: from, To: to}, from + " to " + to, nil
	}
	if month == "" {
		month = s.Now().Format("2006-01")
	}
	p, err := store.MonthPeriod(month)
	if err != nil {
		return p, "", err
	}
	t, _ := time.Parse("2006-01", month)
	return p, t.Format("January 2006"), nil
}

func timeCell(minutes, day int) string {
	if minutes == 0 {
		return "-"
	}
	return duration.Days(minutes, day) + " (" + duration.Hours(minutes) + ")"
}

func amount(p *money.Pence) string {
	if p == nil {
		return "-"
	}
	return p.Display()
}
