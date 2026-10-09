package transport

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/safename"
)

// Downloaded is a verified archive on disk.
type Downloaded struct {
	Path   string // the final path, ready for sevenzip.Open
	URL    string // the URL that served the bytes
	Digest string // the digest that was verified
	Size   int64
}

// Download fetches a repository-relative archive path into destDir and verifies it
// against the digest the official endpoint publishes. The file appears under its
// final name only once it is complete and verified. Give ctx a deadline: an
// archive has no total request timeout.
func (c *Client) Download(ctx context.Context, remote, destDir string) (Downloaded, error) {
	name, err := archiveName(remote)
	if err != nil {
		return Downloaded{}, err
	}
	want, err := c.digestFor(ctx, remote)
	if err != nil {
		return Downloaded{}, err
	}

	var last error
	for _, base := range c.sources {
		src := join(base, remote)
		got, err := try(ctx, c, src, func(ctx context.Context, url string) (Downloaded, error) {
			return c.downloadOne(ctx, url, want, name, destDir)
		})
		if err == nil {
			return got, nil
		}
		if !elsewhere(err) {
			return Downloaded{}, err
		}
		last = err
	}
	if retryable(last) {
		return Downloaded{}, errs.Wrap(exitcode.Network, errs.CodeSourcesExhausted, errs.PhaseDownload, last,
			"no source could serve %s", remote)
	}
	return Downloaded{}, last
}

// archiveName is the file name a repository path ends in, refused unless it is an
// ordinary single file name on every platform.
//
// The path is built from repository metadata, which a mirror supplied, so the last
// segment is not trusted: it must not be a path separator, a control character, a
// character Windows forbids, a name ending in a dot or a space, or a Windows
// device. path.Base splits only on '/', so a '\' survives it and would escape the
// destination through filepath.Join on Windows — the reason this check is by
// content and not by separator count.
//
// The rules are internal/safename's, shared with the extractor and applied to the
// one name this layer writes rather than to an archive entry.
func archiveName(remote string) (string, error) {
	name := path.Base(remote)
	if reason := unsafeName(name); reason != "" {
		return "", errs.New(exitcode.Usage, errs.CodeUnexpectedValue, errs.PhaseConfig,
			"%q does not name an archive file: %s", remote, reason)
	}
	return name, nil
}

// unsafeName says what makes a single name unsafe to write, or "" when it is
// ordinary.
//
// The decision is internal/safename's, shared with the extractor; only the wording
// is local, because a reader here is looking at a download, not an archive entry.
func unsafeName(name string) string {
	switch safename.Check(name) {
	case safename.OK:
		return ""
	case safename.Empty:
		return "it is empty"
	case safename.DotSegment:
		return fmt.Sprintf("it is %q", name)
	case safename.Separator:
		return "it holds a path separator"
	case safename.Control:
		return "it holds a control character"
	case safename.IllegalChar:
		return "it holds a character no file name may"
	case safename.TrailingDotOrSpace:
		return "it ends in a dot or a space"
	case safename.DeviceName:
		return "Windows reads it as a device"
	default:
		return "it is not an ordinary file name"
	}
}

// digestFor reads the expected digest from the official endpoint, never from the
// source that will serve the bytes. When the official endpoint says the path is
// not there, a configured TrustBaseChecksum lets the primary source's own digest
// stand in — the private-base case ADR-007 decision 3 allows.
//
// Both requests use the stay-on-host client. The base is the root of trust once
// its digest is accepted, so it answers to the same rule the official endpoint
// does: a digest that arrives by way of another host is refused either way.
func (c *Client) digestFor(ctx context.Context, remote string) (string, error) {
	suffix := remote + "." + string(c.alg)
	digest, err := c.sidecar(ctx, c.digest, join(c.official, suffix))
	if err == nil {
		return digest, nil
	}
	if !isNotFound(err) {
		return "", err
	}
	if c.trust {
		fromBase, baseErr := c.sidecar(ctx, c.digest, join(c.sources[0], suffix))
		switch {
		case baseErr == nil:
			return fromBase, nil
		case !isNotFound(baseErr):
			return "", baseErr
		}
	}
	return "", errs.New(exitcode.Integrity, errs.CodeChecksumMissing, errs.PhaseVerify,
		"no %s checksum for %s at %s", c.alg, remote, c.official.Host)
}

// sidecar fetches and parses one checksum file, retrying transient failures.
func (c *Client) sidecar(ctx context.Context, client *http.Client, url string) (string, error) {
	return try(ctx, c, url, func(ctx context.Context, u string) (string, error) {
		resp, err := c.do(ctx, client, u)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()
		if err := statusError(u, resp); err != nil {
			return "", err
		}
		body, err := readBounded(ctx, resp, maxSidecarBytes)
		if err != nil {
			return "", err
		}
		return parseSidecar(c.alg, body)
	})
}

// isNotFound reports whether err is the "the server says it is not there" failure.
func isNotFound(err error) bool {
	var e *errs.Error
	return errors.As(err, &e) && e.Code() == errs.CodeHTTPNotFound
}

// downloadOne is a single attempt: stream to a .part beside the target, hash as it
// arrives, verify, then rename. Every failure removes the .part, and the final
// name is never created until the bytes are complete and verified.
func (c *Client) downloadOne(ctx context.Context, url, want, name, destDir string) (Downloaded, error) {
	resp, err := c.do(ctx, c.archive, url)
	if err != nil {
		return Downloaded{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(url, resp); err != nil {
		return Downloaded{}, err
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return Downloaded{}, filesystemError(err)
	}
	final := filepath.Join(destDir, name)
	part := final + ".part"
	file, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return Downloaded{}, filesystemError(err)
	}

	// The hash is computed as the bytes land, so the file is read once. Each
	// attempt truncates and starts a fresh hash, so a retry never mixes streams.
	digest := c.alg.new()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), resp.Body)
	closeErr := file.Close()
	switch {
	case copyErr != nil:
		_ = os.Remove(part)
		return Downloaded{}, requestError(ctx, copyErr)
	case closeErr != nil:
		_ = os.Remove(part)
		return Downloaded{}, filesystemError(closeErr)
	}

	got := hex.EncodeToString(digest.Sum(nil))
	if !equalDigest(got, want) {
		_ = os.Remove(part)
		return Downloaded{}, errs.New(exitcode.Integrity, errs.CodeChecksumMismatch, errs.PhaseVerify,
			"%s: got %s %s, want %s", name, c.alg, got, want)
	}
	if err := os.Rename(part, final); err != nil {
		_ = os.Remove(part)
		return Downloaded{}, filesystemError(err)
	}
	return Downloaded{Path: final, URL: resp.Request.URL.String(), Digest: got, Size: size}, nil
}

// filesystemError is a write, close or rename that could not complete.
func filesystemError(err error) error {
	return errs.Wrap(exitcode.Filesystem, errs.CodeFilesystem, errs.PhaseDownload, err, "writing the archive failed")
}
