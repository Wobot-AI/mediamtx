// Package httphook allows to perform HTTP requests in response to path events.
package httphook

import (
	"context"
	"sync"
	"time"
)

// defaultShutdownTimeout is how long Close() waits for pending requests.
const defaultShutdownTimeout = 10 * time.Second

// entry is a link in the per-key baton chain.
type entry struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Pool serializes HTTP hook requests by key.
//
// Requests with the same key are executed in enqueue order: before running, each
// one cancels its predecessor's context and waits for it to finish. Cancelling is
// both the ordering mechanism and the intended semantics, since a request is only
// ever superseded by one that invalidates it (a "stop" superseding a "start").
type Pool struct {
	// ShutdownTimeout is how long Close waits before cancelling pending requests.
	// Zero means defaultShutdownTimeout.
	ShutdownTimeout time.Duration

	ctx       context.Context
	ctxCancel context.CancelFunc
	mutex     sync.Mutex
	entries   map[string]*entry
	wg        sync.WaitGroup
}

// Initialize initializes a Pool.
func (p *Pool) Initialize() {
	if p.ShutdownTimeout == 0 {
		p.ShutdownTimeout = defaultShutdownTimeout
	}
	p.ctx, p.ctxCancel = context.WithCancel(context.Background())
	p.entries = make(map[string]*entry)
}

// Close waits for pending requests to terminate, then cancels the remaining ones.
func (p *Pool) Close() {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(p.ShutdownTimeout):
		p.ctxCancel()
		<-done
	}

	p.ctxCancel()
}

// Enqueue schedules fn for execution, after any previously enqueued function
// with the same key has terminated.
func (p *Pool) Enqueue(key string, fn func(context.Context)) {
	ctx, cancel := context.WithCancel(p.ctx)
	e := &entry{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	p.mutex.Lock()
	prev := p.entries[key]
	p.entries[key] = e
	p.wg.Add(1)
	p.mutex.Unlock()

	go func() {
		defer p.wg.Done()
		defer func() {
			p.mutex.Lock()
			// the identity check is needed since a successor may have
			// already replaced this entry.
			if p.entries[key] == e {
				delete(p.entries, key)
			}
			p.mutex.Unlock()

			cancel()
			close(e.done)
		}()

		if prev != nil {
			prev.cancel()
			<-prev.done
		}

		fn(ctx)
	}()
}
