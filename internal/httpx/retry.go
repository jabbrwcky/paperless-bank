// Package httpx provides a small retry wrapper around *http.Client for the
// bank and paperless-ngx HTTP clients, which otherwise fail an entire sync
// run on a single transient blip.
package httpx

import (
	"net/http"
	"strconv"
	"time"
)

// maxAttempts is the total number of tries (1 initial + 2 retries), matching
// the retry policy documented in Architecture.md.
const maxAttempts = 3

// Do executes req via client, retrying transient failures: connection errors
// and 5xx responses use short exponential backoff; 429 (rate limited) uses a
// longer backoff and honors a Retry-After header when the server sends one.
// Other status codes (2xx, and permanent 4xx errors like 404/406) are
// returned immediately without retrying.
//
// req.GetBody must be non-nil if req has a body (true automatically for
// bodies created from []byte/*bytes.Buffer/*bytes.Reader/*strings.Reader via
// http.NewRequest, which is how every caller in this codebase builds
// requests) so the body can be replayed on retry.
func Do(client *http.Client, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 && req.GetBody != nil {
			body, gerr := req.GetBody()
			if gerr != nil {
				return nil, gerr
			}
			req.Body = body
		}

		resp, err = client.Do(req)
		if !shouldRetry(resp, err) || attempt == maxAttempts {
			return resp, err
		}

		wait := backoff(attempt, resp)
		if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
	return resp, err
}

func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
}

// backoff picks the wait before the next attempt. 429 gets a longer, separate
// schedule than plain network/5xx errors since it signals a rate limit
// rather than a transient blip, and honors Retry-After (seconds or an
// HTTP-date, per RFC 7231) when the response provides one.
func backoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		if d, ok := retryAfter(resp); ok {
			return d
		}
		return time.Duration(attempt) * 5 * time.Second // 5s, 10s
	}
	return time.Duration(1<<(attempt-1)) * 250 * time.Millisecond // 250ms, 500ms
}

func retryAfter(resp *http.Response) (time.Duration, bool) {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
	}
	return 0, false
}
