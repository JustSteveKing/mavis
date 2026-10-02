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
		Long: `Creates the record folders in dir (default: the current directory):
clients/, engagements/, log/, time/, invoices/ and quotes/, or whatever the
layout in the config names. Then records dir as the root in the config.

It refuses a folder that is, or is inside, a code project (one with a
go.mod, package.json or the like), because records written there would be
committed with the code. A git repository on its own, such as a vault, is
fine.

Safe to run again. --force repoints an existing config at a different
directory, and overrides the code project check.`,
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
			// Checked before anything is created, so a refusal leaves
			// nothing behind.
			if manifest := codeProject(root); manifest != "" && !force {
				return fmt.Errorf("%s looks like a code project (%s), so records kept here would be committed with the code; give mavis a folder of its own, such as ~/business, or pass --force", root, manifest)
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

// manifests mark a folder as a code project.
var manifests = []string{"go.mod", "package.json", "composer.json", "Cargo.toml", "pyproject.toml", "Gemfile", "pom.xml", "build.gradle", "mix.exs"}

// codeProject returns the manifest that makes dir a code project, or a
// project it sits inside: dir itself is checked, then the root of the git
// work tree it is in, if any. A plain git repository with no manifest, such
// as a vault, is not a code project.
func codeProject(dir string) string {
	candidates := []string{dir}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			if d != dir {
				candidates = append(candidates, d)
			}
			break
		}
		if parent := filepath.Dir(d); parent == d {
			break
		}
	}
	for _, c := range candidates {
		for _, m := range manifests {
			if _, err := os.Stat(filepath.Join(c, m)); err == nil {
				return filepath.Join(c, m)
			}
		}
	}
	return ""
}
