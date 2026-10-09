package httphook

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolOrdering(t *testing.T) {
	p := &Pool{}
	p.Initialize()

	var mutex sync.Mutex
	var order []string

	p.Enqueue("k", func(ctx context.Context) {
		// a slow predecessor that ignores cancellation
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
		}
		mutex.Lock()
		order = append(order, "first")
		mutex.Unlock()
	})

	p.Enqueue("k", func(_ context.Context) {
		mutex.Lock()
		order = append(order, "second")
		mutex.Unlock()
	})

	p.Close()

	require.Equal(t, []string{"first", "second"}, order)
}

func TestPoolCancelsPredecessor(t *testing.T) {
	p := &Pool{}
	p.Initialize()

	cancelled := make(chan struct{})

	p.Enqueue("k", func(ctx context.Context) {
		select {
		case <-ctx.Done():
			close(cancelled)
		case <-time.After(10 * time.Second):
		}
	})

	time.Sleep(50 * time.Millisecond)
	p.Enqueue("k", func(_ context.Context) {})

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("predecessor was not cancelled")
	}

	p.Close()
}

func TestPoolDifferentKeysAreConcurrent(t *testing.T) {
	p := &Pool{}
	p.Initialize()

	started := make(chan struct{}, 2)
	release := make(chan struct{})

	for _, k := range []string{"a", "b"} {
		p.Enqueue(k, func(_ context.Context) {
			started <- struct{}{}
			<-release
		})
	}

	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("keys did not run concurrently")
		}
	}

	close(release)
	p.Close()
}

func TestPoolCloseDrains(t *testing.T) {
	p := &Pool{}
	p.Initialize()

	done := make(chan struct{})
	p.Enqueue("k", func(_ context.Context) {
		time.Sleep(200 * time.Millisecond)
		close(done)
	})

	p.Close()

	select {
	case <-done:
	default:
		t.Fatal("Close did not wait for pending work")
	}
}

func TestPoolCloseTimesOut(t *testing.T) {
	p := &Pool{ShutdownTimeout: 200 * time.Millisecond}
	p.Initialize()

	p.Enqueue("k", func(ctx context.Context) {
		<-ctx.Done() // only returns once the pool cancels it
	})

	start := time.Now()
	p.Close()
	elapsed := time.Since(start)

	require.Less(t, elapsed, 3*time.Second)
	require.GreaterOrEqual(t, elapsed, 200*time.Millisecond)
}

// the entry map must not leak once a key goes idle.
func TestPoolReapsEntries(t *testing.T) {
	p := &Pool{}
	p.Initialize()

	for range 5 {
		p.Enqueue("k", func(_ context.Context) {})
	}

	p.Close()

	p.mutex.Lock()
	n := len(p.entries)
	p.mutex.Unlock()

	require.Equal(t, 0, n)
}
