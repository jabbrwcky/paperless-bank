package sync

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
	"github.com/jabbrwcky/paperless-bank/internal/paperless"
)

// DefaultAllowedMIMETypes is used when Orchestrator.AllowedMIMETypes is
// empty. paperless-ngx rejects file types it can't consume (e.g. the
// text/html marketing notices some banks mix into the same inbox as
// statements), so only PDF is safe to assume without configuration.
var DefaultAllowedMIMETypes = []string{"application/pdf"}

// Orchestrator pulls documents from a bank and uploads new ones to paperless-ngx.
type Orchestrator struct {
	Source    bank.DocumentSource
	Paperless *paperless.Client
	// AllowedMIMETypes restricts which documents get uploaded; documents of
	// any other type are skipped, not treated as errors. Empty means
	// DefaultAllowedMIMETypes.
	AllowedMIMETypes []string
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
	if !mimeTypeAllowed(doc.MIMEType, o.allowedMIMETypes()) {
		log.Printf("skip %s (unsupported type %s)", doc.Filename, doc.MIMEType)
		return nil
	}

	exists, err := o.Paperless.DocumentExists(ctx, doc.Filename)
	if err != nil {
		return fmt.Errorf("check existence: %w", err)
	}
	if exists {
		log.Printf("skip %s (already in paperless-ngx)", doc.Filename)
		return nil
	}

	content, err := o.Source.DownloadDocument(ctx, doc)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	if err := o.Paperless.Upload(ctx, doc, content); err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	log.Printf("uploaded %s", doc.Filename)
	return nil
}

func (o *Orchestrator) allowedMIMETypes() []string {
	if len(o.AllowedMIMETypes) > 0 {
		return o.AllowedMIMETypes
	}
	return DefaultAllowedMIMETypes
}

func mimeTypeAllowed(mimeType string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(mimeType), strings.TrimSpace(a)) {
			return true
		}
	}
	return false
}
