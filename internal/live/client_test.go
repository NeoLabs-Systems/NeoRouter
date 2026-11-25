package live

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// client is one client socket of the gateway, with the event helpers the JS test
// uses.
type client struct {
	conn   *websocket.Conn
	opened chan error
	closed chan closeInfo

	mu     sync.Mutex
	events []*object
	wait   chan struct{}
}

// closeInfo is how a socket ended.
type closeInfo struct {
	code   int
	reason string
}

// open connects to the gateway; a refused handshake is reported by opened.
func (f *fixture) open(path string, headers http.Header) *client {
	f.t.Helper()
	c := &client{opened: make(chan error, 1), closed: make(chan closeInfo, 1), wait: make(chan struct{})}
	conn, _, err := websocket.Dial(context.Background(), f.base+path, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		c.opened <- err
		c.closed <- closeInfo{code: -1}
		return c
	}
	c.conn = conn
	c.opened <- nil
	go c.read()
	return c
}

// waitOpened waits for the handshake and fails on a refusal.
func (c *client) waitOpened(t *testing.T) {
	t.Helper()
	select {
	case err := <-c.opened:
		if err != nil {
			t.Fatalf("open: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("open: timed out")
	}
}

func (c *client) read() {
	for {
		_, data, err := c.conn.Read(context.Background())
		if err != nil {
			code, reason := closeError(err)
			c.closed <- closeInfo{code: code, reason: reason}
			return
		}
		event, ok := parseObject(data)
		if !ok {
			continue
		}
		c.mu.Lock()
		c.events = append(c.events, event)
		close(c.wait)
		c.wait = make(chan struct{})
		c.mu.Unlock()
	}
}

// send writes one text message.
func (c *client) send(t *testing.T, payload string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		t.Fatalf("send: %v", err)
	}
}

// close ends the client's socket the way a browser does.
func (c *client) close() {
	if c.conn == nil {
		return
	}
	_ = c.conn.Close(websocket.StatusNormalClosure, "")
}

// next waits for an event of a type, as the JS test's next() does.
func (c *client) next(t *testing.T, kind string) *object {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		c.mu.Lock()
		for _, event := range c.events {
			if event.String("type") == kind {
				c.mu.Unlock()
				return event
			}
		}
		wait := c.wait
		c.mu.Unlock()
		select {
		case <-wait:
			continue
		case <-deadline:
			c.mu.Lock()
			seen := make([]string, 0, len(c.events))
			for _, event := range c.events {
				encoded, _ := marshalObject(event)
				seen = append(seen, string(encoded))
			}
			c.mu.Unlock()
			t.Fatalf("no %q event; got %v", kind, seen)
		}
	}
}

// waitClosed waits for the socket to end.
func (c *client) waitClosed(t *testing.T) closeInfo {
	t.Helper()
	select {
	case info := <-c.closed:
		c.closed <- info
		return info
	case <-time.After(5 * time.Second):
		t.Fatal("socket did not close")
	}
	return closeInfo{}
}
