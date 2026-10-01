package cmd

import (
	"fmt"
	"strings"

	"github.com/JustSteveKing/mavis/internal/pdf"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

func newQuoteCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "quote",
		Aliases: []string{"quotes"},
		Short:   "Draft, send and settle quotes",
	}
	cmd.AddCommand(
		newQuoteNewCommand(a),
		newQuoteListCommand(a),
		newQuoteShowCommand(a),
		newQuoteEditCommand(a),
		newQuoteDiscardCommand(a),
		newQuoteSendCommand(a),
		newQuoteDecideCommand(a, true),
		newQuoteDecideCommand(a, false),
		newQuotePDFCommand(a),
	)
	return cmd
}

func newQuoteNewCommand(a *app) *cobra.Command {
	var in store.NewQuote
	var lines []string
	cmd := &cobra.Command{
		Use:   "new <client>",
		Short: "Draft a quote",
		Long: `Drafts quotes/draft-<client>.md.

The scope is prose: what the client is getting, written above the lines and
printed on the quote. Give it with --scope, or write it in the draft with
` + "`mavis quote edit`" + `. Lines work as they do on invoices, and so does VAT.

A quote stands for 30 days from when it is sent unless --valid says
otherwise.`,
		Example: `  mavis quote new globex --title "Reporting rebuild" \
    --scope "A rebuilt reporting module, with CSV and PDF exports." \
    --line "Discovery and design=3 x 650 day" --line "Build=10 x 650 day"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			in.Client = args[0]
			for _, l := range lines {
				ml, err := parseLineFlag(l)
				if err != nil {
					return err
				}
				in.Lines = append(in.Lines, ml)
			}
			q, err := s.AddQuote(in)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(q)
			}
			a.printf("Drafted %s: %s\n\n", q.Slug, q.Title)
			if len(q.Lines) > 0 {
				a.linesBody(q.Lines, q.VATLines, q.Net, q.Total, q.Currency)
			} else {
				a.printf("No lines yet. Add them with: mavis quote edit %s\n", q.Slug)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Title, "title", "", "what the quote is for (required)")
	f.StringVar(&in.Scope, "scope", "", "what the client gets, as prose")
	f.StringArrayVar(&lines, "line", nil, `a line, "description=price" or "description=qty x price unit"; repeatable`)
	f.IntVar(&in.ValidDays, "valid", store.DefaultValidDays, "days the quote stands once sent")
	return cmd
}

func newQuoteListCommand(a *app) *cobra.Command {
	var client, status string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List quotes",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			all, problems, err := s.Quotes()
			if err != nil {
				return err
			}
			a.warn(problems)
			today := s.Now().Format("2006-01-02")
			shown := []store.Quote{}
			for _, q := range all {
				if (client == "" || q.Client == client) && (status == "" || q.Display(today) == status) {
					shown = append(shown, q)
				}
			}
			if a.jsonOut {
				return a.emitJSON(shown)
			}
			if len(shown) == 0 {
				a.printf("No quotes.\n")
				return nil
			}
			rows := [][]string{{"QUOTE", "STATUS", "CLIENT", "TITLE", "SENT", "VALID UNTIL", "NET"}}
			for _, q := range shown {
				name := q.Number
				if name == "" {
					name = q.Slug
				}
				rows = append(rows, []string{name, q.Display(today), q.Client, q.Title, q.Sent, q.ValidUntil, q.Net.Display() + " " + q.Currency})
			}
			table(a.out, "", map[int]bool{6: true}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&client, "client", "", "only this client's")
	cmd.Flags().StringVar(&status, "status", "", "only this status: draft, sent, expired, accepted or declined")
	return cmd
}

func newQuoteShowCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show <quote>",
		Short: "Show a quote",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			q, err := s.ResolveQuote(args[0])
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(q)
			}
			today := s.Now().Format("2006-01-02")
			name := q.Number
			if name == "" {
				name = "Draft " + q.Slug
			}
			a.printf("%s: %s, %s, %s\n", name, q.Title, q.Client, q.Display(today))
			var facts []string
			if q.Sent != "" {
				facts = append(facts, "sent "+q.Sent)
			}
			if q.ValidUntil != "" {
				facts = append(facts, "valid until "+q.ValidUntil)
			} else {
				facts = append(facts, fmt.Sprintf("valid %d days once sent", q.ValidDays))
			}
			if q.Decided != "" {
				facts = append(facts, q.Status+" "+q.Decided)
			}
			if q.Engagement != "" {
				facts = append(facts, "started "+q.Engagement)
			}
			a.printf("%s\n\n", strings.Join(facts, " · "))
			if q.Scope != "" {
				for _, line := range strings.Split(q.Scope, "\n") {
					a.printf("  %s\n", line)
				}
				a.printf("\n")
			}
			a.linesBody(q.Lines, q.VATLines, q.Net, q.Total, q.Currency)
			return nil
		},
	}
}

func newQuoteEditCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "edit <draft>",
		Short: "Open a draft quote in $EDITOR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			q, err := s.ResolveQuote(args[0])
			if err != nil {
				return err
			}
			if q.Status != "draft" {
				return fmt.Errorf("%s was sent and does not change; draft a new quote instead", q.Number)
			}
			if !interactive() {
				a.printf("%s\n", q.Path)
				return nil
			}
			if err := openEditor(q.Path); err != nil {
				return err
			}
			again, err := s.ResolveQuote(q.Slug)
			if err != nil {
				fmt.Fprintf(a.err, "warning: %v\n", err)
				return nil
			}
			a.linesBody(again.Lines, again.VATLines, again.Net, again.Total, again.Currency)
			return nil
		},
	}
}

func newQuoteDiscardCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "discard <draft>",
		Short: "Delete a draft quote",
		Long:  "Deletes a draft. A sent quote is a record of what was offered: record the answer with accept or decline instead.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			q, err := s.DiscardQuote(args[0])
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(q)
			}
			a.printf("Discarded %s\n", q.Slug)
			return nil
		},
	}
}

func newQuoteSendCommand(a *app) *cobra.Command {
	var date string
	cmd := &cobra.Command{
		Use:   "send <draft>",
		Short: "Number a draft quote, freeze it and write its PDF",
		Long: `Gives a draft the next quote number for the year (Q-2026-001), stamps when
it was sent and when it stops being valid, copies in your details and the
client's, freezes it, and writes its PDF to .invoices/<number>.pdf, beside
your invoices.

It does not email anything. Send the PDF however you send things.

The client's address is not required: a prospect may not have given one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			b := a.cfg.Business
			q, err := s.SendQuote(args[0], store.IssueOptions{
				Issuer:            store.Issuer{Name: b.Name, Address: b.Address, Country: b.Country, VATNumber: b.VATNumber, Email: b.Email, PeppolID: b.PeppolID},
				ReverseChargeNote: a.cfg.Invoicing.ReverseChargeNote,
				Date:              date,
			})
			if err != nil {
				return err
			}
			path, pdfErr := pdf.WriteQuote(s.FilesDir(), q)
			if a.jsonOut {
				return a.emitJSON(struct {
					store.Quote
					PDF string `json:"pdf,omitempty"`
				}{q, path})
			}
			a.printf("Sent %s to %s: %s %s net, valid until %s\n", q.Number, q.ToName, q.Net.Display(), q.Currency, q.ValidUntil)
			if pdfErr != nil {
				return fmt.Errorf("%s is sent, but its PDF failed: %w; try mavis quote pdf %s", q.Number, pdfErr, q.Number)
			}
			a.printf("%s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "when it was sent, YYYY-MM-DD, today or yesterday (default: today)")
	return cmd
}

func newQuoteDecideCommand(a *app, accepted bool) *cobra.Command {
	var date string
	var work store.StartWork
	use, short := "accept <quote>", "Record that a quote was accepted, and start the work"
	if !accepted {
		use, short = "decline <quote>", "Record that a quote was declined"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			var start *store.StartWork
			if work.Name != "" {
				start = &work
			} else if work.Basis != "" || work.Rate != "" {
				return fmt.Errorf("--basis and --rate need --engagement to name the work")
			}
			q, eng, err := s.DecideQuote(args[0], accepted, date, start)
			if err != nil {
				return err
			}
			if a.jsonOut {
				var started *store.Engagement
				if eng.Slug != "" {
					started = &eng
				}
				return a.emitJSON(struct {
					store.Quote
					Started *store.Engagement `json:"started,omitempty"`
				}{q, started})
			}
			a.printf("%s %s on %s\n", q.Number, q.Status, q.Decided)
			if eng.Slug != "" {
				detail := eng.Basis
				if eng.Budget != "" {
					detail += ", budget " + eng.Budget
				}
				if eng.Rate != "" {
					detail += " @ " + eng.Rate
				}
				a.printf("Started %s (%s), %s\n", eng.Title, eng.Slug, detail)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "when it was answered, YYYY-MM-DD, today or yesterday (default: today)")
	if accepted {
		cmd.Long = `Records the acceptance. With --engagement, also starts the work: an active
engagement named <client>-<name>, titled after the quote and linked to it.
By default it is fixed price with the quote's net total as its budget;
--basis day --rate 650 makes it day-rate work instead.`
		cmd.Example = `  mavis quote accept Q-2026-001 --engagement reporting
  mavis quote accept Q-2026-001 --engagement reporting --basis day --rate 650`
		cmd.Flags().StringVar(&work.Name, "engagement", "", "start an engagement with this name")
		cmd.Flags().StringVar(&work.Basis, "basis", "", "the engagement's basis (default: fixed)")
		cmd.Flags().StringVar(&work.Rate, "rate", "", "the engagement's rate, for day or hourly work")
	}
	return cmd
}

func newQuotePDFCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "pdf <quote>",
		Short: "Write a quote's PDF",
		Long:  "Writes .invoices/<number>.pdf, or for a draft a preview headed DRAFT QUOTE. send does this already.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			q, err := s.ResolveQuote(args[0])
			if err != nil {
				return err
			}
			path, err := pdf.WriteQuote(s.FilesDir(), q)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(map[string]string{"quote": args[0], "pdf": path})
			}
			a.printf("%s\n", path)
			return nil
		},
	}
}
