package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type logFlags struct {
	engagement string
	date       string
	with       []string
	followUps  []string
	dues       []string
	noEdit     bool
}

func (f *logFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVarP(&f.engagement, "engagement", "e", "", "the engagement this was about")
	fl.StringVar(&f.date, "date", "", "when it happened, YYYY-MM-DD or YYYY-MM-DDTHH:MM (default: now)")
	fl.StringSliceVar(&f.with, "with", nil, "who was there; repeat or comma-separate")
	fl.StringArrayVarP(&f.followUps, "follow-up", "f", nil, "something to do next; repeatable")
	fl.StringArrayVar(&f.dues, "due", nil, "when the follow-up is due, YYYY-MM-DD or +3d; one for all, or one per follow-up")
	fl.BoolVar(&f.noEdit, "no-edit", false, "do not open $EDITOR when no summary is given")
}

func (f *logFlags) newLog(kind, client, summary string) (store.NewLog, error) {
	in := store.NewLog{Kind: kind, Client: client, Engagement: f.engagement, Summary: summary, Date: f.date, With: f.with}
	switch {
	case len(f.dues) == 0:
	case len(f.followUps) == 0:
		return in, fmt.Errorf("--due needs a --follow-up to be due")
	case len(f.dues) != 1 && len(f.dues) != len(f.followUps):
		return in, fmt.Errorf("give one --due for every follow-up, or a single --due for all of them")
	}
	for i, text := range f.followUps {
		fu := store.NewFollowUp{Text: text}
		if len(f.dues) == 1 {
			fu.Due = f.dues[0]
		} else if len(f.dues) > 0 {
			fu.Due = f.dues[i]
		}
		in.FollowUps = append(in.FollowUps, fu)
	}
	return in, nil
}

func (a *app) addLog(f *logFlags, kind, client string, summaryArgs []string) error {
	s, err := a.openStore()
	if err != nil {
		return err
	}
	summary := strings.Join(summaryArgs, " ")
	in, err := f.newLog(kind, client, summary)
	if err != nil {
		return err
	}
	l, err := s.AddLog(in)
	if err != nil {
		return err
	}

	if summary == "" && !f.noEdit && !a.jsonOut && interactive() {
		if err := openEditor(l.Path); err != nil {
			return fmt.Errorf("logged at %s, but the editor failed: %w", l.Path, err)
		}
	}

	if a.jsonOut {
		return a.emitJSON(l)
	}
	rel, _ := filepath.Rel(s.Root(), l.Path)
	a.printf("Logged %s with %s: %s\n", l.Kind, l.Client, rel)
	if n := len(l.FollowUps); n > 0 {
		a.printf("%d follow-up%s\n", n, plural(n))
	}
	return nil
}

func newLogCommand(a *app) *cobra.Command {
	var f logFlags
	cmd := &cobra.Command{
		Use:   "log <kind> <client> [summary...]",
		Short: "Log a call, meeting, email or note",
		Long: `Writes log/<date>-<client>-<kind>.md.

kind is one of ` + strings.Join(store.LogKinds, ", ") + `. With no summary, opens
$EDITOR on the new entry, unless --no-edit or not at a terminal.

Follow-ups are written as checkboxes. Tick them here with ` + "`mavis done`" + `, or
in any editor.`,
		Example: `  mavis log call acme "Scoped the reporting module" -f "Send estimate" --due +7d
  mavis log meeting acme --with "Jo Bloggs,Sam"`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.addLog(&f, args[0], args[1], args[2:])
		},
	}
	f.register(cmd)
	return cmd
}

func newNoteCommand(a *app) *cobra.Command {
	var f logFlags
	cmd := &cobra.Command{
		Use:     "note <client> [text...]",
		Short:   "Log a note against a client",
		Long:    "Shorthand for `mavis log note`.",
		Example: `  mavis note acme "They are moving to Postgres next quarter"`,
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.addLog(&f, "note", args[0], args[1:])
		},
	}
	f.register(cmd)
	return cmd
}

func newFollowUpsCommand(a *app) *cobra.Command {
	var client string
	var overdue bool
	cmd := &cobra.Command{
		Use:     "follow-ups",
		Aliases: []string{"followups", "fu"},
		Short:   "List open follow-ups, soonest first",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			if client != "" {
				c, err := s.ResolveClient(client)
				if err != nil {
					return err
				}
				client = c.Slug
			}
			open, problems, err := s.FollowUps()
			if err != nil {
				return err
			}
			a.warn(problems)

			today := s.Now().Format("2006-01-02")
			shown := []store.FollowUp{}
			for _, f := range open {
				if client != "" && f.Client != client {
					continue
				}
				if overdue && (f.Due == "" || f.Due >= today) {
					continue
				}
				shown = append(shown, f)
			}
			if a.jsonOut {
				return a.emitJSON(shown)
			}
			if len(shown) == 0 {
				a.printf("Nothing to follow up.\n")
				return nil
			}
			a.followUpTable(shown, today)
			return nil
		},
	}
	cmd.Flags().StringVar(&client, "client", "", "only this client's")
	cmd.Flags().BoolVar(&overdue, "overdue", false, "only those past due")
	return cmd
}

func (a *app) followUpTable(fs []store.FollowUp, today string) {
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DUE\tCLIENT\tFOLLOW-UP\tFROM")
	for _, f := range fs {
		due := f.Due
		if due != "" && due < today {
			due += " !"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", due, f.Client, f.Text, f.Log)
	}
	w.Flush()
}

func newDoneCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "done [client] <follow-up>",
		Short: "Tick off a follow-up",
		Long: `Ticks the one open follow-up whose text contains the words given,
optionally among one client's only. If several match, they are listed and
nothing is ticked.`,
		Example: `  mavis done acme estimate
  mavis done "staging access"`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			client, query := "", args[0]
			if len(args) == 2 {
				client, query = args[0], args[1]
			}
			f, err := s.CompleteFollowUp(client, query)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(f)
			}
			a.printf("Done: %s (%s)\n", f.Text, f.Client)
			return nil
		},
	}
}

// interactive reports whether stdin and stdout are a terminal. Scripts and
// agents get no editor, because nobody is there to close it. A file-mode
// check is not enough: /dev/null is a character device too.
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func openEditor(path string) error {
	editor := strings.Fields(os.Getenv("VISUAL"))
	if len(editor) == 0 {
		editor = strings.Fields(os.Getenv("EDITOR"))
	}
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	c := exec.Command(editor[0], append(editor[1:], path)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
