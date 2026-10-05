package cmd

import (
	"fmt"
	"strings"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

func newDeliveredCommand(a *app) *cobra.Command {
	var date, items string
	cmd := &cobra.Command{
		Use:   "delivered <engagement> [what...]",
		Short: "Record something delivered on item work",
		Long: `For work paid per item, an article or a video: appends a row to the
engagement's sheet for the month, time/<engagement>-<YYYY-MM>.md, beside
any time logged to it. One item unless --items says otherwise.

Only item work takes deliveries; give an engagement basis: item, a rate per
item and a unit with mavis engagement add --basis item --rate 750 --unit
article. Month-end invoices bill what was delivered, and stats value it.`,
		Example: `  mavis delivered sevalla-articles "Laravel queues, a deep dive"
  mavis delivered sevalla-articles "Two short tips" --items 2 --date yesterday`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			e, err := s.AddDelivery(store.NewDelivery{Engagement: args[0], Items: items, What: strings.Join(args[1:], " "), Date: date})
			if err != nil {
				return err
			}
			if by, err := s.InvoiceCovering(e.Engagement, e.Date[:7]); err == nil && by != "" {
				fmt.Fprintf(a.err, "warning: %s for %s is already on %s; this is not on it\n", e.Engagement, e.Date[:7], by)
			}
			if a.jsonOut {
				return a.emitJSON(e)
			}
			unit := store.DefaultUnit
			if eng, err := s.ResolveEngagement(e.Engagement); err == nil {
				unit = eng.UnitName()
			}
			a.printf("Recorded %s %s on %s to %s\n", e.Items, store.Plural(unit, e.Items), e.Date, e.Engagement)
			return nil
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "the day delivered, YYYY-MM-DD, today or yesterday (default: today)")
	cmd.Flags().StringVar(&items, "items", "1", "how many were delivered")
	return cmd
}
