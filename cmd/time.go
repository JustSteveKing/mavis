package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/JustSteveKing/mavis/internal/duration"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

func newTimeCommand(a *app) *cobra.Command {
	var date string
	cmd := &cobra.Command{
		Use:   "time <engagement> <duration> [what...]",
		Short: "Log time against an engagement",
		Long: `Appends a row to the engagement's timesheet for the month, at
time/<engagement>-<YYYY-MM>.md, creating it on the month's first entry.

Duration is 1d, 0.5d, 3h, 45m or 1h30m. A day is day_hours in config, 7.5
unless set. Log days or hours, whichever suits the work; mavis converts.

The sheet is a Markdown table: correct it by hand, or with ` + "`mavis time edit`" + `.`,
		Example: `  mavis time reporting 1d "Report filters"
  mavis time reporting 2h "Call with Jo" --date yesterday
  mavis time list --month 2026-10`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			e, err := s.AddTime(store.NewTime{Engagement: args[0], Duration: args[1], What: strings.Join(args[2:], " "), Date: date})
			if err != nil {
				return err
			}
			if by, err := s.InvoiceCovering(e.Engagement, e.Date[:7]); err == nil && by != "" {
				fmt.Fprintf(a.err, "warning: %s for %s is already on %s; this time is not on it\n", e.Engagement, e.Date[:7], by)
			}
			if a.jsonOut {
				return a.emitJSON(e)
			}
			a.printf("Logged %s on %s to %s\n", e.Time, e.Date, e.Engagement)
			return nil
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "the day worked, YYYY-MM-DD, today or yesterday (default: today)")
	cmd.AddCommand(newTimeListCommand(a), newTimeEditCommand(a))
	return cmd
}

func newTimeListCommand(a *app) *cobra.Command {
	var month, client, engagement string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List time and deliveries for a month",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			if month == "" {
				month = s.Now().Format("2006-01")
			}
			if client != "" {
				c, err := s.ResolveClient(client)
				if err != nil {
					return err
				}
				client = c.Slug
			}
			if engagement != "" {
				e, err := s.ResolveEngagement(engagement)
				if err != nil {
					return err
				}
				engagement = e.Slug
			}
			all, problems, err := s.TimeEntries()
			if err != nil {
				return err
			}
			a.warn(problems)

			shown := []store.TimeEntry{}
			total := 0
			for _, e := range all {
				if month != "all" && !strings.HasPrefix(e.Date, month) {
					continue
				}
				if (client != "" && e.Client != client) || (engagement != "" && e.Engagement != engagement) {
					continue
				}
				shown = append(shown, e)
				total += e.Minutes
			}
			if a.jsonOut {
				return a.emitJSON(shown)
			}
			if len(shown) == 0 {
				a.printf("No time logged.\n")
				return nil
			}
			items := slices.ContainsFunc(shown, func(e store.TimeEntry) bool { return e.Items != "" })
			dash := func(v string) string {
				if v == "" {
					return "-"
				}
				return v
			}
			w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
			if items {
				fmt.Fprintln(w, "DATE\tENGAGEMENT\tITEMS\tTIME\tWHAT")
				for _, e := range shown {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Date, e.Engagement, dash(e.Items), dash(e.Time), e.What)
				}
				fmt.Fprintf(w, "Total\t\t\t%s\t%s\n", duration.Days(total, s.DayMinutes), duration.Hours(total))
				return w.Flush()
			}
			fmt.Fprintln(w, "DATE\tENGAGEMENT\tTIME\tWHAT")
			for _, e := range shown {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Date, e.Engagement, e.Time, e.What)
			}
			fmt.Fprintf(w, "Total\t\t%s\t%s\n", duration.Days(total, s.DayMinutes), duration.Hours(total))
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&month, "month", "", "YYYY-MM, or all (default: this month)")
	cmd.Flags().StringVar(&client, "client", "", "only this client's time")
	cmd.Flags().StringVar(&engagement, "engagement", "", "only this engagement's time")
	return cmd
}

func newTimeEditCommand(a *app) *cobra.Command {
	var month string
	cmd := &cobra.Command{
		Use:   "edit <engagement>",
		Short: "Open a month's timesheet in $EDITOR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			e, err := s.ResolveEngagement(args[0])
			if err != nil {
				return err
			}
			if month == "" {
				month = s.Now().Format("2006-01")
			}
			path := s.SheetPath(e.Slug, month)
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("no time logged to %s in %s", e.Slug, month)
			}
			if !interactive() {
				a.printf("%s\n", path)
				return nil
			}
			return openEditor(path)
		},
	}
	cmd.Flags().StringVar(&month, "month", "", "YYYY-MM (default: this month)")
	return cmd
}
