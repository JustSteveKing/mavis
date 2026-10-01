package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

func newClientCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "client",
		Aliases: []string{"clients"},
		Short:   "Add, list and move clients",
	}
	cmd.AddCommand(newClientAddCommand(a), newClientListCommand(a), newClientShowCommand(a))
	for _, status := range store.ClientStatuses {
		cmd.AddCommand(newClientMoveCommand(a, status))
	}
	return cmd
}

func newClientAddCommand(a *app) *cobra.Command {
	var in store.NewClient
	cmd := &cobra.Command{
		Use:   "add <slug>",
		Short: "Add a client",
		Long: `Adds a client note at clients/<slug>.md.

The slug is the file name and how [[links]] reach the client, so keep it
short: acme, not acme-ltd-uk.`,
		Example: `  mavis client add acme --name "Acme Ltd" --contact "Jo Bloggs" --email jo@acme.test`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			in.Slug = args[0]
			c, err := s.AddClient(in)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(c)
			}
			a.printf("Added %s (%s), %s\n", c.Name, c.Slug, c.Status)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Name, "name", "", "company or person name (default: the slug)")
	f.StringVar(&in.Status, "status", "active", "one of "+strings.Join(store.ClientStatuses, ", "))
	f.StringVar(&in.Contact, "contact", "", "primary contact")
	f.StringVar(&in.Email, "email", "", "contact email")
	f.StringVar(&in.Phone, "phone", "", "contact phone")
	return cmd
}

func newClientListCommand(a *app) *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List clients",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if status != "" && !store.ValidClientStatus(status) {
				return fmt.Errorf("status must be one of %s", strings.Join(store.ClientStatuses, ", "))
			}
			s, err := a.openStore()
			if err != nil {
				return err
			}
			clients, problems, err := s.Clients()
			if err != nil {
				return err
			}
			a.warn(problems)

			shown := []store.Client{}
			for _, c := range clients {
				if status == "" || c.Status == status {
					shown = append(shown, c)
				}
			}
			if a.jsonOut {
				return a.emitJSON(shown)
			}
			if len(shown) == 0 {
				a.printf("No clients.\n")
				return nil
			}
			w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tSTATUS\tSINCE\tNAME\tCONTACT")
			for _, c := range shown {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", c.Slug, c.Status, c.StatusSince, c.Name, c.Contact)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "only clients with this status")
	return cmd
}

func newClientShowCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show <client>",
		Short: "Show a client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			c, err := s.ResolveClient(args[0])
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(c)
			}
			a.printf("%s (%s)\n", c.Name, c.Slug)
			status := c.Status
			if c.StatusSince != "" {
				status += " since " + c.StatusSince
			}
			w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
			for _, row := range [][2]string{
				{"Status", status},
				{"Contact", c.Contact},
				{"Email", c.Email},
				{"Phone", c.Phone},
				{"Terms", fmt.Sprintf("%d days, %s", c.TermsDays, c.Currency)},
				{"File", c.Path},
			} {
				if row[1] != "" {
					fmt.Fprintf(w, "  %s\t%s\n", row[0], row[1])
				}
			}
			return w.Flush()
		},
	}
}

func newClientMoveCommand(a *app, status string) *cobra.Command {
	return &cobra.Command{
		Use:   status + " <client>",
		Short: "Move a client to " + status,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			c, changed, err := s.SetClientStatus(args[0], status)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(c)
			}
			if !changed {
				a.printf("%s is already %s\n", c.Name, status)
				return nil
			}
			a.printf("%s is now %s\n", c.Name, status)
			return nil
		},
	}
}
