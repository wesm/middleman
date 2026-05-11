package gitclone

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
)

// retryAttempts caps how many times a transient git smart-HTTP failure is
// re-issued before surfacing the error to the caller.
const retryAttempts = 3

// isTransientGitError reports whether the stderr captured from git looks
// like a transient remote or network failure worth retrying. GitHub's
// smart-HTTP endpoint sporadically returns 5xx on /info/refs even when the
// repo is healthy; retrying inside the same sync cycle usually succeeds and
// avoids dropping a full sync window.
func isTransientGitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"internal server error",
		"returned error: 500",
		"returned error: 502",
		"returned error: 503",
		"returned error: 504",
		"connection reset",
		"connection refused",
		"could not resolve host",
		"operation timed out",
		"early eof",
		"ssl_read",
		"tls connection",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// defaultBackOff returns the production retry schedule used for transient
// git failures: a few hundred milliseconds first, growing modestly so a
// run of 5xx blips clears inside the sync cycle that triggered them.
func defaultBackOff() *backoff.ExponentialBackOff {
	expo := backoff.NewExponentialBackOff()
	expo.InitialInterval = 500 * time.Millisecond
	expo.MaxInterval = 4 * time.Second
	expo.RandomizationFactor = 0.3
	return expo
}

// retryTransient runs op with bounded exponential backoff, retrying only
// when the underlying error matches isTransientGitError. Permanent errors
// (auth, not-found, malformed remote, etc.) short-circuit immediately so
// they surface to the caller without delay.
func retryTransient[T any](
	ctx context.Context, label string, op func() (T, error),
) (T, error) {
	return retryTransientWithBackOff(ctx, label, defaultBackOff(), op)
}

// retryTransientWithBackOff is the seam used by tests to inject a faster
// schedule; production callers should use retryTransient.
func retryTransientWithBackOff[T any](
	ctx context.Context,
	label string,
	bo backoff.BackOff,
	op func() (T, error),
) (T, error) {
	wrapped := func() (T, error) {
		v, err := op()
		if err == nil {
			return v, nil
		}
		if isTransientGitError(err) {
			return v, err
		}
		return v, backoff.Permanent(err)
	}

	notify := func(err error, next time.Duration) {
		slog.Debug("retrying transient git failure",
			"op", label, "next", next, "err", err)
	}

	return backoff.Retry(ctx, wrapped,
		backoff.WithBackOff(bo),
		backoff.WithMaxTries(retryAttempts),
		backoff.WithNotify(notify),
	)
}
