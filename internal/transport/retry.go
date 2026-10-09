package transport

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// sleeper waits for d, or returns early when ctx ends. It is a field on Client so
// a test can replace it and assert the backoff sequence without waiting.
type sleeper func(ctx context.Context, d time.Duration) error

// realSleep is the sleeper a real Client uses.
func realSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// capped is the exponential delay before attempt n (0-based): Base doubled n
// times, never above max, and 0 when max is not positive.
func capped(base, max time.Duration, attempt int) time.Duration {
	d := base
	for i := 0; i < attempt && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	if d <= 0 {
		return 0
	}
	return d
}

// jittered spreads a delay over [d/2, d] so many clients do not retry in lockstep.
func jittered(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1)) //nolint:gosec // G404: jitter, not secrecy
}

// requestError turns an error from Do or a body read into an *Error with ADR-002's
// code. A refusal produced by checkRedirect already carries its code and is kept.
func requestError(ctx context.Context, err error) error {
	var coded *errs.Error
	if errors.As(err, &coded) {
		return coded
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return errs.Wrap(exitcode.Interrupted, errs.CodeInterrupted, errs.PhaseDownload, err, "the download was cancelled")
	}
	return errs.Wrap(exitcode.Network, errs.CodeRequestFailed, errs.PhaseDownload, err, "the request failed")
}

// retryable reports whether a failure is worth the same URL again after a backoff:
// a connection failure or a transient status.
func retryable(err error) bool {
	var e *errs.Error
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code() {
	case errs.CodeRequestFailed, errs.CodeHTTPStatus:
		return true
	default:
		return false
	}
}

// elsewhere reports whether another source might succeed: a transient failure, a
// source that does not hold the object, or bytes that failed their digest — a
// mirror can be the corrupt one, and a different mirror may serve the same object
// correctly.
func elsewhere(err error) bool {
	if retryable(err) {
		return true
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code() {
	case errs.CodeHTTPNotFound, errs.CodeChecksumMismatch:
		return true
	default:
		return false
	}
}

// try runs fetch against one URL, backing off between attempts, and returns the
// first success or the last failure. It stops early on a failure retrying cannot
// fix.
func try[T any](ctx context.Context, c *Client, url string, fetch func(context.Context, string) (T, error)) (T, error) {
	var zero T
	var last error
	for attempt := 0; attempt < c.retry.Attempts; attempt++ {
		if attempt > 0 {
			if err := c.sleep(ctx, jittered(capped(c.retry.Base, c.retry.Max, attempt-1))); err != nil {
				return zero, requestError(ctx, err)
			}
		}
		value, err := fetch(ctx, url)
		if err == nil {
			return value, nil
		}
		last = err
		if !retryable(err) {
			return zero, err
		}
	}
	return zero, last
}
