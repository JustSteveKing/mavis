package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

func newTodayCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "today",
		Short: "What needs attention: follow-ups, active work, quiet clients",
		Long: `Overdue follow-ups and those due in the next seven days, active
engagements, and clients worth a look:

  active, with no active engagement, quiet for ` + "`thresholds.active_quiet`" + ` days: move to warm?
  warm, quiet for ` + "`thresholds.warm_keep_in_touch`" + ` days: get in touch
  warm, quiet for ` + "`thresholds.warm_to_cold`" + ` days: move to cold?

Quiet counts from the later of the last log entry and the last status move.
mavis suggests moves; it never makes them. Cold clients are never shown.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			th := a.cfg.Thresholds
			t, problems, err := s.Today(store.Quiet{
				ActiveQuiet:     th.ActiveQuiet,
				WarmKeepInTouch: th.WarmKeepInTouch,
				WarmToCold:      th.WarmToCold,
			})
			if err != nil {
				return err
			}
			a.warn(problems)
			if a.jsonOut {
				return a.emitJSON(t)
			}

			empty := true
			section := func(title string) *tabwriter.Writer {
				if !empty {
					a.printf("\n")
				}
				empty = false
				a.printf("%s\n", title)
				return tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
			}

			if len(t.Overdue) > 0 {
				w := section("Overdue")
				for _, f := range t.Overdue {
					fmt.Fprintf(w, "  %s\t%s · due %s (%s)\n", f.Text, f.Client, f.Due, ago(f.Due, t.Date))
				}
				w.Flush()
			}
			if len(t.ThisWeek) > 0 {
				w := section("Due this week")
				for _, f := range t.ThisWeek {
					fmt.Fprintf(w, "  %s\t%s · due %s\n", f.Text, f.Client, f.Due)
				}
				w.Flush()
			}
			if len(t.Engagements) > 0 {
				w := section("Active engagements")
				for _, e := range t.Engagements {
					detail := e.Basis
					if e.Start != "" {
						if detail != "" {
							detail += " · "
						}
						detail += "since " + e.Start
					}
					fmt.Fprintf(w, "  %s\t%s\t%s\n", e.Client, e.Title, detail)
				}
				w.Flush()
			}
			if len(t.Moves) > 0 {
				w := section("Worth a move?")
				for _, n := range t.Moves {
					fmt.Fprintf(w, "  %s\t%s → %s?\n", n.Client, n.Reason, n.Suggest)
				}
				w.Flush()
			}
			if len(t.KeepInTouch) > 0 {
				w := section("Keep in touch")
				for _, n := range t.KeepInTouch {
					last := "no contact logged"
					if n.LastContact != "" {
						last = "last contact " + n.LastContact
					}
					fmt.Fprintf(w, "  %s\t%s · %s\n", n.Client, n.Reason, last)
				}
				w.Flush()
			}
			if empty {
				a.printf("Nothing needs you today.\n")
			}
			return nil
		},
	}
}

func ago(date, today string) string {
	n, ok := daysFrom(date, today)
	if !ok {
		return ""
	}
	if n == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", n)
}

func daysFrom(date, today string) (int, bool) {
	d, err1 := time.Parse("2006-01-02", date)
	t, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return int(t.Sub(d).Hours() / 24), true
}
