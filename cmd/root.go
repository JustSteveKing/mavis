// Package cmd holds the mavis command line.
//
// Commands stay thin: parse flags, call into the store, render. Logic that
// lives here instead of in the store is logic the MCP server and the TUI
// cannot reach.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/JustSteveKing/mavis/internal/config"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

// app carries the state a command tree needs.
//
// A struct rather than package-level variables, so each NewRootCommand gets
// its own flag targets and tests cannot leak state into one another.
type app struct {
	cfg     *config.Config
	root    string
	jsonOut bool

	out io.Writer
	err io.Writer
}

func (a *app) openStore() (*store.Store, error) {
	root, err := a.cfg.ResolveRoot(a.root)
	if err != nil {
		return nil, err
	}
	s, err := store.Open(root)
	if err != nil {
		return nil, err
	}
	s.DayMinutes = a.cfg.DayMinutes()
	return s, nil
}

// emitJSON is the single place JSON output is produced, so every command's
// machine-readable form is shaped the same way.
func (a *app) emitJSON(v any) error {
	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (a *app) printf(format string, args ...any) {
	fmt.Fprintf(a.out, format, args...)
}

// warn reports files that could not be read, without stopping the command.
func (a *app) warn(problems []store.Problem) {
	for _, p := range problems {
		fmt.Fprintf(a.err, "warning: skipped %s\n", p)
	}
}

func NewRootCommand(version string) *cobra.Command {
	a := &app{out: os.Stdout, err: os.Stderr}

	root := &cobra.Command{
		Use:   "mavis",
		Short: "Run a freelance business from plain files",
		Long: `mavis keeps clients, engagements, calls and notes as Markdown files in a
folder or an Obsidian vault.

Every record is a file you can open, grep, diff and edit by hand.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			a.out = cmd.OutOrStdout()
			a.err = cmd.ErrOrStderr()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			a.cfg = cfg
			return nil
		},
	}

	root.PersistentFlags().StringVar(&a.root, "root", "", "directory holding the records (default from config, or $MAVIS_ROOT)")
	root.PersistentFlags().BoolVar(&a.jsonOut, "json", false, "output JSON")

	root.AddCommand(
		newInitCommand(a),
		newClientCommand(a),
		newEngagementCommand(a),
		newLogCommand(a),
		newNoteCommand(a),
		newFollowUpsCommand(a),
		newDoneCommand(a),
		newTodayCommand(a),
		newTimeCommand(a),
		newStatsCommand(a),
		newInvoiceCommand(a),
		newQuoteCommand(a),
		newMCPCommand(a, version),
	)
	return root
}

// Execute runs the command tree and exits non-zero on error.
func Execute(version string) {
	root := NewRootCommand(version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "mavis:", err)
		os.Exit(1)
	}
}
