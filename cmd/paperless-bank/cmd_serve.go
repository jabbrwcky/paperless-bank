package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/paperless"
	"github.com/jabbrwcky/paperless-bank/internal/server"
)

// ServeCmd runs paperless-bank as a long-lived server: it syncs on an
// interval and exposes a minimal web UI for completing bank authentication
// challenges (e.g. a TAN) when no terminal is attached.
type ServeCmd struct {
	Listen       string        `help:"Address to bind the web UI to" default:"127.0.0.1:8080"`
	SyncInterval time.Duration `help:"How often to run sync automatically" default:"1h"`
	AutoReauth   bool          `help:"Automatically start a live re-authentication (a real bank login and TAN challenge) via the web UI when the cached token is invalid or expired. Off by default: an invalid token instead just marks the bank \"action required\", requiring an explicit 'paperless-bank auth <bank>' run" default:"false"`
}

func (s *ServeCmd) Run(cli *CLI) error {
	if cli.Paperless.URL == "" || cli.Paperless.Token == "" {
		return fmt.Errorf("paperless-url and paperless-token are required for serve")
	}

	configured := cli.configuredBanks()
	if len(configured) == 0 {
		return fmt.Errorf("no banks configured")
	}

	srv := &server.Server{
		Paperless:    paperless.New(cli.Paperless.URL, cli.Paperless.Token),
		Banks:        configured,
		SyncInterval: s.SyncInterval,
		AutoReauth:   s.AutoReauth,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return srv.Run(ctx, s.Listen)
}
