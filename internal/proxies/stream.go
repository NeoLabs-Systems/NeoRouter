package proxies

// The response lease, ported from server/services/proxies/stream.js: a proxy's
// active slot is held for the whole response, including SSE, and read on demand
// so a large download never buffers a second copy.

import (
	"io"
	"sync"
)

// leasedBody releases the proxy slot when the body finishes, fails or is closed.
type leasedBody struct {
	rc      io.ReadCloser
	release func()
	failed  func()

	mu   sync.Mutex
	done bool
}

func (b *leasedBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	switch {
	case err == nil:
		return n, nil
	case err == io.EOF:
		b.finish(false)
	default:
		// A read failure after the response began is a transport failure: the
		// proxy may have dropped mid-stream, so it goes back to rest.
		b.finish(true)
	}
	return n, err
}

func (b *leasedBody) Close() error {
	err := b.rc.Close()
	b.finish(false)
	return err
}

func (b *leasedBody) finish(fail bool) {
	b.mu.Lock()
	if b.done {
		b.mu.Unlock()
		return
	}
	b.done = true
	b.mu.Unlock()
	if fail {
		b.failed()
	}
	b.release()
}
