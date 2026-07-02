package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

// ListCmd prints available documents from all configured banks without uploading.
type ListCmd struct {
	Bank string `optional:"" help:"Restrict listing to a specific bank (default: all configured)"`
}

func (l *ListCmd) Run(cli *CLI) error {
	ctx := context.Background()

	sources := cli.configuredBanks()
	if l.Bank != "" {
		cfg := cli.bankConfig(l.Bank)
		if cfg == nil {
			return fmt.Errorf("no configuration found for bank %q", l.Bank)
		}
		sources = map[string]bank.Config{l.Bank: *cfg}
	}
	if len(sources) == 0 {
		return fmt.Errorf("no banks configured")
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "BANK\tDATE\tFILENAME\tDESCRIPTION")

	for name, cfg := range sources {
		src, err := bank.New(name, cfg)
		if err != nil {
			return err
		}
		docs, err := src.ListDocuments(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		for _, doc := range docs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				name,
				doc.Date.Format("2006-01-02"),
				doc.Filename,
				doc.Description,
			)
		}
	}

	return w.Flush()
}
