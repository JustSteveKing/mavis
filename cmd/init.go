package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

func newInitCommand(a *app) *cobra.Command {
	var force, yes bool
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Set up a records directory and remember it",
		Long: `Creates the record folders in dir (default: the current directory):
clients/, engagements/, log/, time/, invoices/ and quotes/, or whatever the
layout in the config names. Then records dir as the root in the config.

In a new or empty folder it also runs git init, so the records can be kept
under version control from the start; mavis never commits for you. In a
folder already inside a git repository it asks first, because the records
will be tracked by that repository, and says so louder if the repository
holds code. Without a terminal to ask on, pass --yes. A folder that has
files in it but is not a repository is left as it is.

Safe to run again. --force repoints an existing config at a different
directory.`,
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
			if a.cfg.Root != "" && a.cfg.Root != root && !force {
				return fmt.Errorf("config already points at %s; pass --force to point it at %s", a.cfg.Root, root)
			}

			// Everything is decided before anything is created, so a no
			// leaves nothing behind.
			repo := gitRoot(root)
			startRepo := repo == "" && isEmptyOrMissing(root)
			if repo != "" && !yes {
				ok, err := confirmInRepo(root, repo)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("nothing created")
				}
			}

			if err := os.MkdirAll(root, 0o755); err != nil {
				return err
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

			git := ""
			if startRepo {
				git = startGit(root)
			}

			if a.jsonOut {
				return a.emitJSON(map[string]string{"root": root, "config": a.cfg.File(), "repository": repo, "git": git})
			}
			a.printf("Records in %s\nConfig at %s\n", root, a.cfg.File())
			switch {
			case repo != "":
				a.printf("Tracked by the git repository at %s\n", repo)
			case git != "":
				a.printf("%s\n", git)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "repoint an existing config at this directory")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "initialise inside an existing git repository without asking")
	return cmd
}

// confirmInRepo asks before putting records where an existing repository
// will track them. It cannot ask without a terminal, so then it refuses.
func confirmInRepo(root, repo string) (bool, error) {
	why := "The records will be tracked by that repository."
	if m := manifest(repo); m != "" {
		why = fmt.Sprintf("It holds code (%s), so the records would be committed alongside it. That is rarely what you want.", filepath.Base(m))
	}
	if !interactive() {
		return false, fmt.Errorf("%s is inside the git repository at %s. %s Pass --yes to initialise there anyway", root, repo, why)
	}
	ok := false
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("%s is inside the git repository at %s.", root, repo)).
			Description(why + " Initialise here?").
			Affirmative("Initialise here").Negative("Cancel").
			Value(&ok),
	)).WithTheme(huh.ThemeBase16()).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	return ok, err
}

// gitRoot is the work tree dir is in, or "" when it is in none. dir need not
// exist yet: the walk starts from it either way.
func gitRoot(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

func isEmptyOrMissing(dir string) bool {
	entries, err := os.ReadDir(dir)
	return errors.Is(err, os.ErrNotExist) || (err == nil && len(entries) == 0)
}

// startGit runs git init and says what happened. Not having git is not an
// error: version control is offered, not required.
func startGit(root string) string {
	git, err := exec.LookPath("git")
	if err != nil {
		return "git is not installed, so no repository was started"
	}
	if out, err := exec.Command(git, "init", "--quiet", root).CombinedOutput(); err != nil {
		return fmt.Sprintf("git init failed, so no repository was started: %s", out)
	}
	return "Started a git repository there; mavis never commits for you"
}

// manifests mark a repository as holding code.
var manifests = []string{"go.mod", "package.json", "composer.json", "Cargo.toml", "pyproject.toml", "Gemfile", "pom.xml", "build.gradle", "mix.exs"}

func manifest(dir string) string {
	for _, m := range manifests {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return filepath.Join(dir, m)
		}
	}
	return ""
}
