package mcpserver

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Run serves over stdio until the client disconnects or ctx is cancelled.
//
// It returns nil when the client simply went away. The SDK reports that as
// a JSON-RPC wire error with io.EOF only in its message text, and the chain
// ends in a type this package cannot import, so errors.Is does not match.
// taskgo found that the hard way. Rather than match an internal string, this
// watches stdin itself and treats a shutdown after EOF as a normal ending:
// exiting non-zero on every disconnect makes a clean exit look like a crash.
func Run(ctx context.Context, s *store.Store, o Options, version string) error {
	stdin := &eofWatcher{r: os.Stdin}
	transport := &mcp.IOTransport{
		Reader: stdin,
		// stdout is the protocol channel; the transport must not close it.
		Writer: nopWriteCloser{os.Stdout},
	}
	err := New(s, o, version).Run(ctx, transport)
	if err != nil && stdin.seen() {
		return nil
	}
	return err
}

type eofWatcher struct {
	r      io.Reader
	sawEOF atomic.Bool
}

func (e *eofWatcher) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if errors.Is(err, io.EOF) {
		e.sawEOF.Store(true)
	}
	return n, err
}

func (e *eofWatcher) Close() error { return nil }
func (e *eofWatcher) seen() bool   { return e.sawEOF.Load() }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
