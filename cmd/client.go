package cmd

import (
	"fmt"
	"slices"
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
	cmd.AddCommand(newClientAddCommand(a), newClientListCommand(a), newClientShowCommand(a), newClientSetCommand(a))
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
			all, problems, err := s.Engagements()
			if err != nil {
				return err
			}
			a.warn(problems)
			engagements := []store.Engagement{}
			for _, e := range all {
				if e.Client == c.Slug {
					engagements = append(engagements, e)
				}
			}

			logs, problems, err := s.Logs()
			if err != nil {
				return err
			}
			a.warn(problems)
			recent := []store.LogEntry{}
			open := []store.FollowUp{}
			for _, l := range logs {
				if l.Client != c.Slug {
					continue
				}
				recent = append(recent, l)
				for _, f := range l.FollowUps {
					if !f.Done {
						open = append(open, f)
					}
				}
			}
			slices.Reverse(recent)
			if len(recent) > 5 {
				recent = recent[:5]
			}

			if a.jsonOut {
				return a.emitJSON(struct {
					store.Client
					Engagements []store.Engagement `json:"engagements"`
					RecentLog   []store.LogEntry   `json:"recent_log"`
					FollowUps   []store.FollowUp   `json:"follow_ups"`
				}{c, engagements, recent, open})
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
				{"Address", strings.Join(c.Address, ", ")},
				{"Country", c.Country},
				{"VAT number", c.VATNumber},
				{"VAT", c.Treatment()},
				{"Peppol ID", c.PeppolID},
				{"Buyer ref", c.BuyerReference},
				{"Invoicing", invoicingLabel(c)},
				{"File", c.Path},
			} {
				if row[1] != "" {
					fmt.Fprintf(w, "  %s\t%s\n", row[0], row[1])
				}
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if len(engagements) > 0 {
				a.printf("\nEngagements\n")
				a.engagementTable(engagements)
			}
			if len(open) > 0 {
				a.printf("\nFollow-ups\n")
				a.followUpTable(open, s.Now().Format("2006-01-02"))
			}
			if len(recent) > 0 {
				a.printf("\nRecent\n")
				w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
				for _, l := range recent {
					fmt.Fprintf(w, "  %s\t%s\t%s\n", l.Date, l.Kind, firstLine(l.Summary))
				}
				return w.Flush()
			}
			return nil
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

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	if len(line) > 72 {
		line = line[:69] + "..."
	}
	return line
}

func newClientSetCommand(a *app) *cobra.Command {
	var u store.ClientUpdate
	var name, contact, email, phone, currency, country, vatNumber, vatTreatment, peppolID, buyerRef, invoicing string
	var terms int
	var address []string
	cmd := &cobra.Command{
		Use:   "set <client>",
		Short: "Change a client's details",
		Long: `Changes only the fields given; everything else in the note, including
anything added by hand, is kept. An empty value removes a field.

Billing details (address, country, VAT number) are only needed once you
invoice the client. VAT treatment follows the country unless set: standard
for GB, reverse charge for anywhere else.

--invoicing names where a client is invoiced when it is not mavis, such as
FreeAgent or Upwork. mavis then refuses to draft invoices for them and
leaves their retainers out of today; --invoicing mavis clears it.`,
		Example: `  mavis client set acme --address "1 High Street" --address "Manchester M1 1AA" --country GB
  mavis client set globex --country DE --vat-number DE123456789
  mavis client set acme --phone ""
  mavis client set mozilla --invoicing Upwork`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			for flag, target := range map[string]struct {
				dst **string
				val *string
			}{
				"name": {&u.Name, &name}, "contact": {&u.Contact, &contact}, "email": {&u.Email, &email},
				"phone": {&u.Phone, &phone}, "currency": {&u.Currency, &currency}, "country": {&u.Country, &country},
				"vat-number": {&u.VATNumber, &vatNumber}, "vat-treatment": {&u.VATTreatment, &vatTreatment},
				"peppol-id": {&u.PeppolID, &peppolID}, "buyer-reference": {&u.BuyerReference, &buyerRef},
				"invoicing": {&u.Invoicing, &invoicing},
			} {
				if f.Changed(flag) {
					*target.dst = target.val
				}
			}
			if f.Changed("terms") {
				u.TermsDays = &terms
			}
			if f.Changed("address") {
				u.Address = address
				if u.Address == nil || (len(address) == 1 && address[0] == "") {
					u.Address = []string{}
				}
			}
			s, err := a.openStore()
			if err != nil {
				return err
			}
			c, err := s.SetClient(args[0], u)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(c)
			}
			a.printf("Updated %s\n", c.Name)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "company or person name")
	f.StringVar(&contact, "contact", "", "primary contact")
	f.StringVar(&email, "email", "", "contact email")
	f.StringVar(&phone, "phone", "", "contact phone")
	f.StringVar(&currency, "currency", "", "three-letter code, e.g. GBP")
	f.IntVar(&terms, "terms", 0, "payment terms in days")
	f.StringArrayVar(&address, "address", nil, "an address line; repeat for each line, in order")
	f.StringVar(&country, "country", "", "two-letter code, e.g. GB")
	f.StringVar(&vatNumber, "vat-number", "", "the client's VAT number")
	f.StringVar(&vatTreatment, "vat-treatment", "", "override: "+strings.Join(store.VATTreatments, ", "))
	f.StringVar(&peppolID, "peppol-id", "", "for e-invoices: their Peppol participant, scheme:value, e.g. 9932:GB123456789")
	f.StringVar(&buyerRef, "buyer-reference", "", "for e-invoices: the reference they want on invoices, e.g. a PO number")
	f.StringVar(&invoicing, "invoicing", "", "where they are invoiced when not by mavis, e.g. FreeAgent; mavis clears it")
	return cmd
}

// invoicingLabel says where a client is invoiced, when it is not mavis.
func invoicingLabel(c store.Client) string {
	if !c.InvoicedElsewhere() {
		return ""
	}
	return c.Invoicing + ", not mavis"
}
