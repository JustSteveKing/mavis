package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/JustSteveKing/mavis/internal/mcpserver"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

func newMCPCommand(a *app, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve mavis to an AI agent over MCP, on stdio",
		Long: `Runs the MCP server, so an agent such as Claude can read and keep your
records. It is started by the agent's client, not by hand:

    claude mcp add mavis -- mavis mcp

Agents can read everything, keep clients, engagements and the log, log
time, and draft invoices, quotes, credit notes and reminders. They cannot
issue, send, mark paid, or answer a quote: those stay with you, here.
Everything an agent creates is stamped by: agent.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			s.Actor = "agent"

			// stdout is the protocol channel: anything else written there
			// corrupts the session, so diagnostics go to stderr.
			fmt.Fprintf(os.Stderr, "mavis mcp: serving %s\n", s.Root())
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			th := a.cfg.Thresholds
			o := mcpserver.Options{
				Quiet:     store.Quiet{ActiveQuiet: th.ActiveQuiet, WarmKeepInTouch: th.WarmKeepInTouch, WarmToCold: th.WarmToCold},
				Signoff:   a.cfg.Business.Name,
				Statutory: a.cfg.Invoicing.StatutoryNotice,
			}
			if err := mcpserver.Run(ctx, s, o, version); err != nil && ctx.Err() == nil {
				return err
			}
			return nil
		},
	}
}
