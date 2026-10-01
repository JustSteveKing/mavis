package cmd

import (
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/JustSteveKing/mavis/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func newTUICommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive view",
		Long: `Today, clients, invoices and quotes in one screen, with a detail pane that
follows the cursor. It re-reads the files every two seconds, so a call an
agent logs over MCP, or a note you add in Obsidian, appears on its own.

From it you can tick off follow-ups, add notes, mark invoices paid (after a
y/n) and preview reminders. Issuing, sending and recording reminders stay
at the CLI. ? lists the keys.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			th := a.cfg.Thresholds
			m := tui.New(s, tui.Options{
				Quiet:     store.Quiet{ActiveQuiet: th.ActiveQuiet, WarmKeepInTouch: th.WarmKeepInTouch, WarmToCold: th.WarmToCold},
				Signoff:   a.cfg.Business.Name,
				Statutory: a.cfg.Invoicing.StatutoryNotice,
			})
			_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
			return err
		},
	}
}
