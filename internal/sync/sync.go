package sync

import (
	"context"
	"fmt"
	"log"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
	"github.com/jabbrwcky/paperless-bank/internal/paperless"
)

// Orchestrator pulls documents from a bank and uploads new ones to paperless-ngx.
type Orchestrator struct {
	Source    bank.DocumentSource
	Paperless *paperless.Client
}

// Run fetches all documents from the bank and uploads any that paperless-ngx
// does not already have. Errors are collected per document; a single failure
// does not abort the remaining uploads.
func (o *Orchestrator) Run(ctx context.Context) error {
	docs, err := o.Source.ListDocuments(ctx)
	if err != nil {
		return fmt.Errorf("list documents: %w", err)
	}

	log.Printf("found %d document(s)", len(docs))

	var failed int
	for _, doc := range docs {
		if err := o.syncDoc(ctx, doc); err != nil {
			log.Printf("error [%s]: %v", doc.Filename, err)
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d document(s) failed to sync", failed, len(docs))
	}
	return nil
}

func (o *Orchestrator) syncDoc(ctx context.Context, doc bank.Document) error {
	exists, err := o.Paperless.DocumentExists(ctx, doc.Filename)
	if err != nil {
		return fmt.Errorf("check existence: %w", err)
	}
	if exists {
		log.Printf("skip %s (already in paperless-ngx)", doc.Filename)
		return nil
	}

	content, err := o.Source.DownloadDocument(ctx, doc.ID)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	if err := o.Paperless.Upload(ctx, doc, content); err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	log.Printf("uploaded %s", doc.Filename)
	return nil
}
