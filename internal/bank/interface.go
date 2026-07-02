package bank

import (
	"context"
	"time"
)

// Document is a bank inbox/postbox document.
type Document struct {
	ID          string
	Filename    string
	MIMEType    string
	Date        time.Time
	Description string
}

// DocumentSource is implemented by each bank integration.
// The interface lives here (the consumer), not in the implementing package.
type DocumentSource interface {
	ListDocuments(ctx context.Context) ([]Document, error)
	DownloadDocument(ctx context.Context, id string) ([]byte, error)
}

// Authenticator is an optional extension for banks that require an
// interactive auth step (e.g. TAN challenge). AuthCmd checks for this.
type Authenticator interface {
	Authenticate(ctx context.Context) error
}
