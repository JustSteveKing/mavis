// Package cmd holds the mavis command line.
//
// Commands stay thin: parse flags, call into the store, render. Logic that
// lives here instead of in the store is logic the MCP server and the TUI
// cannot reach.
package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// app carries the state a command tree needs.
//
// A struct rather than package-level variables, so each NewRootCommand gets
// its own flag targets and tests cannot leak state into one another.
type app struct {
	root    string
	jsonOut bool

	out io.Writer
	err io.Writer
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
			return nil
		},
	}

	root.PersistentFlags().StringVar(&a.root, "root", "", "directory holding the records (default from config, or $MAVIS_ROOT)")
	root.PersistentFlags().BoolVar(&a.jsonOut, "json", false, "output JSON")

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
