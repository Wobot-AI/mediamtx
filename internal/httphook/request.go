package httphook

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bluenviron/mediamtx/internal/logger"
)

const (
	maxResponseBodySize = 128 * 1024
	maxRetryInterval    = 30 * time.Second
)

// Outcome is the result of a Request.
type Outcome int

const (
	// OutcomeSuccess means the server accepted the request.
	OutcomeSuccess Outcome = iota

	// OutcomeRejected means the server definitively refused the request.
	// The remote side is known not to have acted on it.
	OutcomeRejected

	// OutcomeUnknown means it is not known whether the server acted on the
	// request: it was cancelled, timed out, or failed in a transient way until
	// the retry budget ran out.
	OutcomeUnknown
)

// Request is an HTTP hook request.
type Request struct {
	Address       string
	Method        string
	Headers       []string
	Body          string
	Timeout       time.Duration
	Retries       int
	RetryInterval time.Duration
	// Deadline bounds the total time spent retrying. Zero means no bound.
	Deadline time.Time
	Logger   logger.Writer
	// Desc identifies the hook in logs, e.g. "runOnDemandHTTP".
	Desc string
}

// Do performs the request, retrying transient failures.
func (r *Request) Do(ctx context.Context) Outcome {
	interval := r.RetryInterval

	for attempt := 0; ; attempt++ {
		retryable, err := r.doOnce(ctx)
		if err == nil {
			r.Logger.Log(logger.Debug, "%s: %s %s succeeded", r.Desc, r.method(), r.Address)
			return OutcomeSuccess
		}

		if !retryable {
			r.Logger.Log(logger.Error, "%s: %v", r.Desc, err)
			if ctx.Err() != nil {
				return OutcomeUnknown
			}
			return OutcomeRejected
		}

		if ctx.Err() != nil {
			r.Logger.Log(logger.Warn, "%s: aborted: %v", r.Desc, err)
			return OutcomeUnknown
		}

		if attempt >= r.Retries {
			r.Logger.Log(logger.Error, "%s: giving up after %d attempts: %v", r.Desc, attempt+1, err)
			return OutcomeUnknown
		}

		if !r.Deadline.IsZero() && !time.Now().Add(interval).Before(r.Deadline) {
			r.Logger.Log(logger.Error, "%s: giving up, deadline reached: %v", r.Desc, err)
			return OutcomeUnknown
		}

		r.Logger.Log(logger.Warn, "%s: %v; retrying in %v", r.Desc, err, interval)

		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return OutcomeUnknown
		}

		interval *= 2
		if interval > maxRetryInterval {
			interval = maxRetryInterval
		}
	}
}

func (r *Request) method() string {
	if r.Method == "" {
		return http.MethodPost
	}
	return r.Method
}

// doOnce performs a single attempt. It returns whether the failure is worth retrying.
func (r *Request) doOnce(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	var body io.Reader
	if r.Body != "" {
		body = bytes.NewReader([]byte(r.Body))
	}

	req, err := http.NewRequestWithContext(ctx, r.method(), r.Address, body)
	if err != nil {
		return false, fmt.Errorf("unable to create request: %w", err)
	}

	for _, h := range r.Headers {
		key, value, ok := strings.Cut(h, ":")
		if !ok {
			return false, fmt.Errorf("invalid header: '%s'", key)
		}
		req.Header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}

	tr := &http.Transport{}
	defer tr.CloseIdleConnections()

	res, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return true, fmt.Errorf("request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 200 && res.StatusCode <= 299 {
		return false, nil
	}

	resBody, err := io.ReadAll(&limitReader{r: res.Body, n: maxResponseBodySize})
	if err != nil {
		resBody = nil
	}

	err = fmt.Errorf("server replied with code %d: %s", res.StatusCode, string(resBody))

	return isRetryableStatus(res.StatusCode), err
}

func isRetryableStatus(code int) bool {
	switch {
	case code == http.StatusRequestTimeout, code == http.StatusTooManyRequests:
		return true

	case code >= 500:
		return true

	default:
		return false
	}
}

// like io.LimitReader, but returns a dedicated error if the limit is exceeded.
type limitReader struct {
	r io.Reader
	n int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, fmt.Errorf("size exceeds maximum allowed")
	}
	if int64(len(p)) > l.n {
		p = p[0:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}
