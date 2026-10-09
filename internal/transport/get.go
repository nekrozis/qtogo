package transport

import (
	"context"
	"io"
	"net/http"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

const (
	// maxDocumentBytes bounds a small object held in memory. A listing or an
	// Updates.xml is a few hundred kilobytes, so this is generous.
	maxDocumentBytes = 8 << 20
	// maxSidecarBytes bounds a checksum file, which is a line or two.
	maxSidecarBytes = 64 << 10
)

// Document is a small object fetched into memory, with the URL that answered it,
// so a caller can resolve relative links or label a failure.
type Document struct {
	URL  string
	Body []byte
}

// Get fetches a repository-relative path, trying each source in order.
func (c *Client) Get(ctx context.Context, path string) (Document, error) {
	return overSources(ctx, c, path, c.getOne)
}

// getOne fetches one URL's bytes, with no retry of its own.
func (c *Client) getOne(ctx context.Context, url string) (Document, error) {
	resp, err := c.do(ctx, c.small, url)
	if err != nil {
		return Document{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(url, resp); err != nil {
		return Document{}, err
	}
	body, err := readBounded(ctx, resp, maxDocumentBytes)
	if err != nil {
		return Document{}, err
	}
	return Document{URL: resp.Request.URL.String(), Body: body}, nil
}

// do issues a context-carrying GET and classifies a transport-level failure.
func (c *Client) do(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errs.Wrap(exitcode.Usage, errs.CodeUnsupportedScheme, errs.PhaseConfig, err, "request for %q", url)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, requestError(ctx, err)
	}
	return resp, nil
}

// readBounded reads a response body, refusing one larger than max.
func readBounded(ctx context.Context, resp *http.Response, max int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, requestError(ctx, err)
	}
	if int64(len(body)) > max {
		return nil, errs.New(exitcode.Network, errs.CodeDocumentTooLarge, errs.PhaseDownload,
			"%s is larger than %d bytes", resp.Request.URL, max)
	}
	return body, nil
}

// statusError reports a non-2xx response. A 404 is the server saying the object
// is not there — ADR-002's not-found — and any other non-2xx is a network
// failure.
func statusError(url string, resp *http.Response) error {
	if resp.StatusCode/100 == 2 {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return errs.New(exitcode.NotFound, errs.CodeHTTPNotFound, errs.PhaseDownload,
			"%s: not found (HTTP %d)", url, resp.StatusCode)
	}
	return errs.New(exitcode.Network, errs.CodeHTTPStatus, errs.PhaseDownload,
		"%s: HTTP %d %s", url, resp.StatusCode, http.StatusText(resp.StatusCode))
}

// overSources tries a path against each source in order, backing off within one,
// and stops early on a failure no other source could fix. A source that lacks the
// object, or whose bytes failed their digest, is passed over; a refused scheme or
// a redirect violation ends the attempt.
func overSources[T any](ctx context.Context, c *Client, path string, one func(context.Context, string) (T, error)) (T, error) {
	var zero T
	var last error
	for _, base := range c.sources {
		value, err := try(ctx, c, join(base, path), one)
		if err == nil {
			return value, nil
		}
		if !elsewhere(err) {
			return zero, err
		}
		last = err
	}
	return zero, last
}
