package main

import (
	"context"
	"fmt"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
	"github.com/jabbrwcky/paperless-bank/internal/paperless"
	"github.com/jabbrwcky/paperless-bank/internal/sync"
)

// SyncCmd fetches new documents from all configured banks and uploads them to paperless-ngx.
type SyncCmd struct {
	MimeTypes []string `name:"mime-types" help:"Comma-separated MIME types allowed for upload; documents of any other type are skipped" default:"application/pdf" env:"PAPERLESS_BANK_MIME_TYPES"`
}

func (s *SyncCmd) Run(cli *CLI) error {
	if cli.Paperless.URL == "" || cli.Paperless.Token == "" {
		return fmt.Errorf("paperless-url and paperless-token are required for sync")
	}

	client := paperless.New(cli.Paperless.URL, cli.Paperless.Token)
	ctx := context.Background()

	// For v1 only comdirect is supported. Additional banks are added here
	// alongside the corresponding config block in CLI.
	configured := cli.configuredBanks()
	if len(configured) == 0 {
		return fmt.Errorf("no banks configured")
	}

	var firstErr error
	for name, cfg := range configured {
		src, err := bank.New(name, cfg)
		if err != nil {
			return err
		}
		orch := &sync.Orchestrator{
			Source:           src,
			Paperless:        client,
			AllowedMIMETypes: s.MimeTypes,
			WatermarkPath:    sync.DefaultWatermarkPath(name),
		}
		if err := orch.Run(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
