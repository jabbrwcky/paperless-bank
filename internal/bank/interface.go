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
	// DownloadDocument fetches the raw bytes of doc. The full Document (not
	// just its ID) is required because some banks' download endpoints need
	// the expected MIME type up front (e.g. via an Accept header).
	DownloadDocument(ctx context.Context, doc Document) ([]byte, error)
}

// Challenge describes one step of an interactive authentication flow that
// needs input from the user (e.g. a bank's TAN/2FA challenge).
type Challenge struct {
	// Description is a human-readable label, e.g. "photoTAN — scan the
	// graphic with your photoTAN app/reader".
	Description string
	// Image is an optional challenge graphic (e.g. a decoded photoTAN PNG).
	Image []byte
	// Hint is optional supplementary text (e.g. a masked phone number).
	Hint string
	// NeedsInput is false for pure notifications (e.g. "approve the push in
	// your app") where no typed value is expected back.
	NeedsInput bool
}

// ChallengeHandler lets a bank's Authenticate implementation delegate the
// interactive part of a challenge to whatever is driving it (CLI prompt, web
// UI, ...), so bank packages have no direct I/O dependency of their own.
type ChallengeHandler interface {
	// Handle presents challenge to the user. When challenge.NeedsInput is
	// true it blocks until the user responds or ctx is done, returning the
	// typed value; otherwise it may return immediately after recording or
	// displaying the notification.
	Handle(ctx context.Context, challenge Challenge) (string, error)
}

// Authenticator is an optional extension for banks that require an
// interactive auth step (e.g. TAN challenge). AuthCmd checks for this.
type Authenticator interface {
	Authenticate(ctx context.Context, handler ChallengeHandler) error
}

// TokenChecker is an optional extension for banks that cache a token/session
// and can verify or silently refresh it without user interaction. When
// EnsureAuthenticated fails, callers that also implement Authenticator can
// fall back to a full interactive Authenticate.
type TokenChecker interface {
	EnsureAuthenticated(ctx context.Context) error
}
