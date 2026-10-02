package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/JustSteveKing/mavis/internal/update"
	"github.com/spf13/cobra"
)

// newUpdateClient is replaced in tests.
var newUpdateClient = update.Default

func newUpdateCommand(a *app, version string) *cobra.Command {
	var check, force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update mavis to the latest release",
		Long: `Checks GitHub for the latest mavis release and, if it is newer than this
one, downloads it for this machine, checks it against the release's
checksums and puts it in place of the mavis you ran.

--check only says whether there is an update. A build of your own (from
make install or go build) is not a release, so update leaves it alone
unless you pass --force.

Set GITHUB_TOKEN if GitHub's rate limit for anonymous requests gets in the
way.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newUpdateClient()
			latest, err := c.Latest(cmd.Context())
			if err != nil {
				return err
			}
			available := force || update.Newer(version, latest.Version)
			if a.jsonOut && check {
				return a.emitJSON(map[string]any{
					"current":          version,
					"latest":           latest.Version,
					"update_available": update.Newer(version, latest.Version),
					"url":              latest.URL,
				})
			}
			if !update.IsRelease(version) && !force {
				a.printf("This mavis is a local build (%s), not a release. The latest release is %s.\n", version, latest.Version)
				a.printf("Run mavis update --force to replace it with %s.\n", latest.Version)
				return nil
			}
			if !available {
				a.printf("mavis %s is the latest release.\n", version)
				return nil
			}
			if check {
				a.printf("mavis %s is out; you have %s.\n%s\nRun mavis update to install it.\n", latest.Version, version, latest.URL)
				return nil
			}

			self, err := os.Executable()
			if err != nil {
				return err
			}
			if self, err = filepath.EvalSymlinks(self); err != nil {
				return err
			}
			a.printf("Updating mavis %s to %s for %s/%s...\n", version, latest.Version, runtime.GOOS, runtime.GOARCH)
			binary, err := c.Fetch(cmd.Context(), latest, runtime.GOOS, runtime.GOARCH)
			if err != nil {
				return err
			}
			if err := update.Replace(self, binary); err != nil {
				return err
			}
			out, err := exec.Command(self, "--version").Output()
			if err != nil {
				return fmt.Errorf("installed %s, but it does not run: %w", self, err)
			}
			a.printf("Installed %s: %s\nWhat changed: %s\n", self, strings.TrimSpace(string(out)), latest.URL)
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only say whether there is a newer release")
	cmd.Flags().BoolVar(&force, "force", false, "replace a local build, or reinstall, with the latest release")
	return cmd
}
