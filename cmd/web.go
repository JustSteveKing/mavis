package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/JustSteveKing/mavis/internal/web"
	"github.com/spf13/cobra"
)

func newWebCommand(a *app) *cobra.Command {
	var (
		port   int
		noOpen bool
	)
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Browse the records in your web browser",
		Long: `Serves the records as a small read-only site on this machine and opens it in
your default browser: today, clients, engagements, the log, follow-ups,
time, invoices, quotes and stats, all linked to one another.

Every page reads the files again, so reload to see a change made at the
CLI, in the TUI, by an agent or in Obsidian. Nothing in the browser can
change a record.

It listens on 127.0.0.1 only and answers only to localhost, so nothing
else on your network, and no other site open in your browser, can read it.
Ctrl-C stops it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			th := a.cfg.Thresholds
			h, err := web.New(s, web.Options{
				Quiet:    store.Quiet{ActiveQuiet: th.ActiveQuiet, WarmKeepInTouch: th.WarmKeepInTouch, WarmToCold: th.WarmToCold},
				ErrorLog: log.New(a.err, "mavis web: ", 0),
			})
			if err != nil {
				return err
			}
			ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				return fmt.Errorf("could not listen on port %d: %w", port, err)
			}
			url := fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)
			a.printf("Serving %s at %s\nCtrl-C to stop.\n", s.Root(), url)

			if !noOpen {
				if err := openBrowser(url); err != nil {
					fmt.Fprintf(a.err, "Could not open a browser (%v). Open %s yourself.\n", err, url)
				}
			}

			srv := &http.Server{
				Handler:           h,
				ReadHeaderTimeout: 10 * time.Second,
				ErrorLog:          log.New(a.err, "mavis web: ", 0),
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				srv.Shutdown(shut)
			}()
			if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "port to listen on (default: any free port)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open a browser, just serve")
	return cmd
}

// openBrowser hands the URL to the desktop's default browser.
func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait()
	return nil
}
