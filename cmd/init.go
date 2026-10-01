package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
)

func newInitCommand(a *app) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Set up a records directory and remember it",
		Long: `Creates clients/, engagements/ and log/ in dir (default: the current
directory) and records dir as the root in the config file.

Safe to run again. It will not repoint an existing config at a different
directory unless --force is given.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(root, 0o755); err != nil {
				return err
			}

			if a.cfg.Root != "" && a.cfg.Root != root && !force {
				return fmt.Errorf("config already points at %s; pass --force to point it at %s", a.cfg.Root, root)
			}

			s, err := store.Open(root)
			if err != nil {
				return err
			}
			if err := s.SetLayout(a.cfg.Layout); err != nil {
				return fmt.Errorf("%s: %w", a.cfg.File(), err)
			}
			if err := s.Init(); err != nil {
				return err
			}
			if err := a.cfg.SaveRoot(root); err != nil {
				return err
			}

			if a.jsonOut {
				return a.emitJSON(map[string]string{"root": root, "config": a.cfg.File()})
			}
			a.printf("Records in %s\nConfig at %s\n", root, a.cfg.File())
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "repoint an existing config at this directory")
	return cmd
}
