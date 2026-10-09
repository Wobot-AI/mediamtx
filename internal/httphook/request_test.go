package httphook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bluenviron/mediamtx/internal/logger"
)

type nilLogger struct{}

func (nilLogger) Log(_ logger.Level, _ string, _ ...any) {}

func TestRequestSuccess(t *testing.T) {
	var gotMethod, gotKey, gotBody, gotCT string

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotKey = r.Header.Get("x-api-key")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	req := &Request{
		Address: s.URL,
		Headers: []string{"Content-Type: application/json", "x-api-key: secret123"},
		Body:    `{"camera":"cam1","type":"start"}`,
		Timeout: 2 * time.Second,
		Logger:  nilLogger{},
		Desc:    "test",
	}

	require.Equal(t, OutcomeSuccess, req.Do(context.Background()))
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "secret123", gotKey)
	require.Equal(t, "application/json", gotCT)
	require.Equal(t, `{"camera":"cam1","type":"start"}`, gotBody)
}

func TestRequestRetriesServerError(t *testing.T) {
	var count atomic.Int32

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if count.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	req := &Request{
		Address:       s.URL,
		Timeout:       2 * time.Second,
		Retries:       5,
		RetryInterval: 10 * time.Millisecond,
		Logger:        nilLogger{},
		Desc:          "test",
	}

	require.Equal(t, OutcomeSuccess, req.Do(context.Background()))
	require.Equal(t, int32(3), count.Load())
}

func TestRequestDoesNotRetryUnauthorized(t *testing.T) {
	var count atomic.Int32

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer s.Close()

	req := &Request{
		Address:       s.URL,
		Timeout:       2 * time.Second,
		Retries:       5,
		RetryInterval: 10 * time.Millisecond,
		Logger:        nilLogger{},
		Desc:          "test",
	}

	require.Equal(t, OutcomeRejected, req.Do(context.Background()))
	require.Equal(t, int32(1), count.Load())
}

func TestRequestExhaustedRetriesIsUnknown(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer s.Close()

	req := &Request{
		Address:       s.URL,
		Timeout:       2 * time.Second,
		Retries:       2,
		RetryInterval: 10 * time.Millisecond,
		Logger:        nilLogger{},
		Desc:          "test",
	}

	require.Equal(t, OutcomeUnknown, req.Do(context.Background()))
}

func TestRequestDeadlineBoundsRetries(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer s.Close()

	req := &Request{
		Address:       s.URL,
		Timeout:       2 * time.Second,
		Retries:       100,
		RetryInterval: 100 * time.Millisecond,
		Deadline:      time.Now().Add(300 * time.Millisecond),
		Logger:        nilLogger{},
		Desc:          "test",
	}

	start := time.Now()
	require.Equal(t, OutcomeUnknown, req.Do(context.Background()))
	require.Less(t, time.Since(start), 3*time.Second)
}

func TestRequestCancellationIsUnknown(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())

	req := &Request{
		Address:       s.URL,
		Timeout:       2 * time.Second,
		Retries:       100,
		RetryInterval: 50 * time.Millisecond,
		Logger:        nilLogger{},
		Desc:          "test",
	}

	go func() {
		time.Sleep(120 * time.Millisecond)
		cancel()
	}()

	require.Equal(t, OutcomeUnknown, req.Do(ctx))
}

func TestRequestTransportErrorIsUnknown(t *testing.T) {
	req := &Request{
		Address:       "http://127.0.0.1:1/nope",
		Timeout:       200 * time.Millisecond,
		Retries:       1,
		RetryInterval: 10 * time.Millisecond,
		Logger:        nilLogger{},
		Desc:          "test",
	}

	require.Equal(t, OutcomeUnknown, req.Do(context.Background()))
}
