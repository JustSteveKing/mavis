package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

func newEngagementCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "engagement",
		Aliases: []string{"engagements", "eng"},
		Short:   "Add, list and move pieces of work for a client",
	}
	cmd.AddCommand(newEngagementAddCommand(a), newEngagementListCommand(a), newEngagementShowCommand(a))
	for _, status := range store.EngagementStatuses {
		cmd.AddCommand(newEngagementMoveCommand(a, status))
	}
	return cmd
}

func newEngagementAddCommand(a *app) *cobra.Command {
	var in store.NewEngagement
	cmd := &cobra.Command{
		Use:   "add <client> <name>",
		Short: "Add an engagement",
		Long: `Adds engagements/<client>-<name>.md.

Basis and rate are optional: they only matter once you track time or
invoice.`,
		Example: `  mavis engagement add acme reporting --title "Reporting module" --basis day --rate 650`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			in.Client, in.Name = args[0], args[1]
			e, err := s.AddEngagement(in)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(e)
			}
			a.printf("Added %s (%s) for %s, %s\n", e.Title, e.Slug, e.Client, e.Status)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Title, "title", "", "what the work is (default: the name)")
	f.StringVar(&in.Status, "status", "active", "one of "+strings.Join(store.EngagementStatuses, ", "))
	f.StringVar(&in.Basis, "basis", "", "one of "+strings.Join(store.Bases, ", "))
	f.StringVar(&in.Rate, "rate", "", "per day or hour; for a retainer, per month")
	f.StringVar(&in.Budget, "budget", "", "the agreed price of fixed-price work, or a cap on the rest")
	f.StringVar(&in.Start, "start", "", "start date, YYYY-MM-DD (default: today, if active)")
	f.StringVar(&in.Project, "project", "", "the project note this work belongs to")
	return cmd
}

func newEngagementListCommand(a *app) *cobra.Command {
	var client, status string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List engagements",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if status != "" && !store.ValidEngagementStatus(status) {
				return fmt.Errorf("status must be one of %s", strings.Join(store.EngagementStatuses, ", "))
			}
			s, err := a.openStore()
			if err != nil {
				return err
			}
			if client != "" {
				c, err := s.ResolveClient(client)
				if err != nil {
					return err
				}
				client = c.Slug
			}
			all, problems, err := s.Engagements()
			if err != nil {
				return err
			}
			a.warn(problems)

			shown := []store.Engagement{}
			for _, e := range all {
				if (client == "" || e.Client == client) && (status == "" || e.Status == status) {
					shown = append(shown, e)
				}
			}
			if a.jsonOut {
				return a.emitJSON(shown)
			}
			if len(shown) == 0 {
				a.printf("No engagements.\n")
				return nil
			}
			a.engagementTable(shown)
			return nil
		},
	}
	cmd.Flags().StringVar(&client, "client", "", "only this client's engagements")
	cmd.Flags().StringVar(&status, "status", "", "only engagements with this status")
	return cmd
}

func (a *app) engagementTable(es []store.Engagement) {
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SLUG\tSTATUS\tCLIENT\tTITLE\tBASIS\tSTART")
	for _, e := range es {
		basis := e.Basis
		if e.Rate != "" {
			basis += " @ " + e.Rate
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Slug, e.Status, e.Client, e.Title, basis, e.Start)
	}
	w.Flush()
}

func newEngagementShowCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show <engagement>",
		Short: "Show an engagement",
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
			if a.jsonOut {
				return a.emitJSON(e)
			}
			a.printf("%s (%s)\n", e.Title, e.Slug)
			basis := e.Basis
			if e.Rate != "" {
				basis += " @ " + e.Rate
			}
			w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
			for _, row := range [][2]string{
				{"Client", e.Client},
				{"Status", e.Status},
				{"Basis", basis},
				{"Budget", e.Budget},
				{"Start", e.Start},
				{"End", e.End},
				{"Project", e.Project},
				{"File", e.Path},
			} {
				if row[1] != "" {
					fmt.Fprintf(w, "  %s\t%s\n", row[0], row[1])
				}
			}
			return w.Flush()
		},
	}
}

func newEngagementMoveCommand(a *app, status string) *cobra.Command {
	return &cobra.Command{
		Use:   status + " <engagement>",
		Short: "Move an engagement to " + status,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			e, changed, err := s.SetEngagementStatus(args[0], status)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(e)
			}
			if !changed {
				a.printf("%s is already %s\n", e.Title, status)
				return nil
			}
			a.printf("%s is now %s\n", e.Title, status)
			return nil
		},
	}
}
