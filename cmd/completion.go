package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// Cobra's `completion <shell>` prints a script and stops there, because
// where it belongs depends on the shell and how it is set up. install puts
// it there.

var shells = []string{"bash", "zsh", "fish"}

func addCompletionInstall(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	var completion *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			completion = c
		}
	}
	if completion == nil {
		return
	}
	var yes bool
	install := &cobra.Command{
		Use:   "install [bash|zsh|fish]",
		Short: "Install completions where your shell will load them",
		Long: `Writes the completion script for your shell (from $SHELL unless named)
to the folder that shell loads completions from, so it works in every new
shell with nothing to source by hand:

  bash  ~/.local/share/bash-completion/completions/mavis
        loaded by bash-completion, which must be installed
  zsh   ~/.local/share/zsh/site-functions/_mavis
        loaded once that folder is on zsh's fpath; if it is not, this asks
        before adding it to ~/.zshrc, or prints the lines without a terminal
  fish  ~/.config/fish/completions/mavis.fish
        loaded by fish on its own

Run it again after upgrading mavis to refresh the script.`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: shells,
		RunE: func(cmd *cobra.Command, args []string) error {
			shell, err := pickShell(args)
			if err != nil {
				return err
			}
			path, err := completionPath(shell)
			if err != nil {
				return err
			}
			var script bytes.Buffer
			switch shell {
			case "bash":
				err = root.GenBashCompletionV2(&script, true)
			case "zsh":
				err = root.GenZshCompletion(&script)
			case "fish":
				err = root.GenFishCompletion(&script, true)
			}
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, script.Bytes(), 0o644); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Installed %s completions to %s\n", shell, path)
			switch shell {
			case "bash":
				if !hasBashCompletion() {
					fmt.Fprintln(out, "bash-completion does not seem to be installed, and bash only loads this through it. Install it (e.g. pacman -S bash-completion, apt install bash-completion, brew install bash-completion@2).")
				}
				fmt.Fprintln(out, "Open a new shell, or load it now with: source "+path)
			case "zsh":
				return zshFpath(out, filepath.Dir(path), yes)
			case "fish":
				fmt.Fprintln(out, "Open a new shell and it is loaded.")
			}
			return nil
		},
	}
	install.Flags().BoolVarP(&yes, "yes", "y", false, "zsh: add the folder to ~/.zshrc without asking")

	uninstall := &cobra.Command{
		Use:       "uninstall [bash|zsh|fish]",
		Short:     "Remove installed completions",
		Long:      "Removes the script install wrote. Lines install added to ~/.zshrc are left for you to remove; they are marked.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: shells,
		RunE: func(cmd *cobra.Command, args []string) error {
			shell, err := pickShell(args)
			if err != nil {
				return err
			}
			path, err := completionPath(shell)
			if err != nil {
				return err
			}
			if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(cmd.OutOrStdout(), "No %s completions installed at %s\n", shell, path)
				return nil
			} else if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", path)
			return nil
		},
	}
	completion.AddCommand(install, uninstall)
}

func pickShell(args []string) (string, error) {
	shell := ""
	if len(args) == 1 {
		shell = args[0]
	} else {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	if !slices.Contains(shells, shell) {
		if len(args) == 0 {
			return "", fmt.Errorf("cannot tell your shell from $SHELL (%q); name it: mavis completion install bash|zsh|fish", os.Getenv("SHELL"))
		}
		return "", fmt.Errorf("%q: install supports %s", shell, strings.Join(shells, ", "))
	}
	return shell, nil
}

func xdg(env, fallback string) (string, error) {
	if d := os.Getenv(env); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
}

func completionPath(shell string) (string, error) {
	switch shell {
	case "bash":
		d, err := xdg("XDG_DATA_HOME", ".local/share")
		return filepath.Join(d, "bash-completion", "completions", "mavis"), err
	case "zsh":
		d, err := xdg("XDG_DATA_HOME", ".local/share")
		return filepath.Join(d, "zsh", "site-functions", "_mavis"), err
	default:
		d, err := xdg("XDG_CONFIG_HOME", ".config")
		return filepath.Join(d, "fish", "completions", "mavis.fish"), err
	}
}

func hasBashCompletion() bool {
	for _, p := range []string{
		"/usr/share/bash-completion/bash_completion", "/etc/bash_completion",
		"/opt/homebrew/etc/profile.d/bash_completion.sh", "/usr/local/etc/profile.d/bash_completion.sh",
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// zshFpath makes sure dir is on zsh's fpath. It asks zsh itself, as an
// interactive shell so ~/.zshrc is read, and only touches ~/.zshrc with
// permission: it is the user's file.
func zshFpath(out io.Writer, dir string, yes bool) error {
	if onZshFpath(dir) {
		fmt.Fprintln(out, "Open a new shell and it is loaded.")
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	rc := filepath.Join(home, ".zshrc")
	block := fmt.Sprintf("\n# Added by mavis completion install: load completions from %s.\nfpath=(%s $fpath)\nautoload -Uz compinit && compinit\n", dir, dir)

	if !yes {
		if !interactive() {
			fmt.Fprintf(out, "%s is not on zsh's fpath, so zsh will not load it yet. Add this to %s, or run again with --yes to have it added:\n%s", dir, rc, block)
			return nil
		}
		ok := false
		err := huh.NewForm(huh.NewGroup(huh.NewConfirm().
			Title(dir + " is not on zsh's fpath, so zsh will not load the completions yet.").
			Description("Add it to " + rc + "? These lines go at the end:" + strings.ReplaceAll(block, "\n", "\n  ")).
			Affirmative("Add them").Negative("I'll do it").Value(&ok),
		)).WithTheme(huh.ThemeBase16()).Run()
		if err != nil && !errors.Is(err, huh.ErrUserAborted) {
			return err
		}
		if !ok {
			fmt.Fprintf(out, "Left %s alone. Add this when you are ready:\n%s", rc, block)
			return nil
		}
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(block); err != nil {
		return err
	}
	fmt.Fprintf(out, "Added %s to the fpath in %s. Open a new shell and it is loaded.\n", dir, rc)
	return nil
}

func onZshFpath(dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "zsh", "-ic", "print -rl -- $fpath").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if filepath.Clean(strings.TrimSpace(line)) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
