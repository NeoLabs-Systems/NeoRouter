package pools

// A member's side of the relay (relay/connector.js): one WebSocket per pool to
// the pool's host, kept open (heartbeats, reconnecting with growing pauses), over
// which partners reach this member and this member reaches partners that have no
// public address either. It goes out from here, so it works behind NAT.

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// retryMin is the first pause before knocking again, retryMax the longest.
	retryMin = time.Second
	retryMax = time.Minute
	// dialTimeout bounds the WebSocket handshake.
	dialTimeout = 10 * time.Second
)

// connectorSet keeps one connection per (person, pool).
type connectorSet struct {
	service *Service
	mu      sync.Mutex
	byKey   map[string]*connector
}

func newConnectorSet(service *Service) *connectorSet {
	return &connectorSet{service: service, byKey: map[string]*connector{}}
}

// connector is one member's connection to one pool's host.
type connector struct {
	service      *Service
	userID       string
	poolID       string
	hostURL      string
	hostMemberID string
	checkHost    func(ctx context.Context, userID, origin string) error
	onUnknown    func(ctx context.Context, memberID string) error

	endpoint *relayEndpoint

	mu       sync.Mutex
	ws       *relaySocket
	ready    bool
	stopped  bool
	failures int
	online   map[string]bool
	waiters  []chan bool
	timer    *time.Timer
}

// relayURL is the WebSocket address of a pool host's relay.
func relayURL(hostURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(hostURL))
	if err != nil {
		return ""
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	parsed.Path = RelayPath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// send delivers one frame to the host.
func (c *connector) send(frame any) error {
	c.mu.Lock()
	ready, ws := c.ready, c.ws
	c.mu.Unlock()
	if !ready || ws == nil {
		return errors.New("not connected to the pool host")
	}
	return ws.send(encodeFrame(frame))
}

// connect opens the connection (once per attempt; retry schedules the next).
func (c *connector) connect(ctx context.Context) {
	c.mu.Lock()
	stopped := c.stopped
	c.mu.Unlock()
	if stopped {
		return
	}
	if c.checkHost != nil {
		if err := c.checkHost(ctx, c.userID, c.hostURL); err != nil {
			c.service.log().Warn("Pool host is not an allowed address for the relay",
				"poolId", c.poolID, "error", err)
			c.retry()
			return
		}
	}
	c.mu.Lock()
	stopped = c.stopped
	c.mu.Unlock()
	// Left the pool while the address was checked.
	if stopped {
		return
	}
	header := http.Header{}
	header.Set("User-Agent", PeerUserAgent)
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	ws, _, err := websocket.Dial(dialCtx, relayURL(c.hostURL), &websocket.DialOptions{
		HTTPHeader: header,
	})
	cancel()
	if err != nil {
		c.retry()
		return
	}
	ws.SetReadLimit(int64(relayLimits.FrameBytes))
	socket := &relaySocket{ws: ws, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go socket.writer()

	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		socket.terminate()
		return
	}
	c.ws = socket
	c.mu.Unlock()

	// Heartbeats find connections that died without closing: a ping that is not
	// answered in time drops the connection, and the retry loop reconnects.
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Duration(relayLimits.HeartbeatMS) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(context.Background(), relayWriteTimeout)
				err := ws.Ping(pingCtx)
				pingCancel()
				if err != nil {
					socket.terminate()
					return
				}
			}
		}
	}()

	for {
		messageType, data, err := socket.read(ctx)
		if err != nil {
			code := 0
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				code = int(closeErr.Code)
			}
			close(heartbeatDone)
			c.onClose(socket, code)
			return
		}
		if messageType != websocket.MessageText {
			continue
		}
		frame, ok := parseRelayFrame(string(data))
		if !ok {
			continue
		}
		c.onFrame(frame)
	}
}

// onClose handles a connection that went away.
func (c *connector) onClose(socket *relaySocket, code int) {
	c.mu.Lock()
	if c.ws != socket {
		c.mu.Unlock()
		return
	}
	c.ready = false
	c.ws = nil
	c.online = map[string]bool{}
	// Not (or no longer) welcome there: no point in knocking again soon.
	if code == relayCloseNotMember || code == relayCloseStopped {
		c.failures = int(math.Max(float64(c.failures), 5))
	}
	c.mu.Unlock()
	socket.terminate()
	c.endpoint.closeAll("")
	c.retry()
}

// onFrame handles one frame from the host.
func (c *connector) onFrame(frame relayFrame) {
	switch frame.typ() {
	case "challenge":
		c.mu.Lock()
		ready := c.ready
		c.mu.Unlock()
		if ready {
			return
		}
		nonce, _ := frame.text("nonce")
		record, err := c.service.ensureIdentity(context.Background(), c.userID)
		if err != nil {
			return
		}
		signature, err := c.service.sign(context.Background(), c.userID,
			helloText(nonce, c.poolID, record.MemberID, c.hostMemberID))
		if err != nil {
			return
		}
		// The hello goes straight out: the connection is not "ready" until the host
		// answers it.
		c.mu.Lock()
		ws := c.ws
		c.mu.Unlock()
		if ws != nil {
			_ = ws.send(encodeFrame(helloFrame{T: "hello", Pool: c.poolID, Member: record.MemberID, Sig: signature}))
		}
		return
	case "ready":
		online := map[string]bool{}
		for _, id := range frame.members("online") {
			online[id] = true
		}
		c.mu.Lock()
		c.ready = true
		c.failures = 0
		c.online = online
		waiters := c.waiters
		c.waiters = nil
		c.mu.Unlock()
		for _, waiter := range waiters {
			select {
			case waiter <- true:
			default:
			}
		}
		return
	case "presence":
		member, _ := frame.text("member")
		online := frame.boolean("online")
		c.mu.Lock()
		if online {
			c.online[member] = true
		} else {
			delete(c.online, member)
		}
		c.mu.Unlock()
		c.service.onPresence(presenceEvent{UserID: c.userID, PoolID: c.poolID, MemberID: member, Online: online})
		return
	}
	c.mu.Lock()
	ready := c.ready
	c.mu.Unlock()
	if ready {
		c.endpoint.handleFrame(frame)
	}
}

// retry schedules the next attempt with a growing pause.
func (c *connector) retry() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.failures++
	exponent := math.Min(float64(c.failures-1), 6)
	delay := math.Min(float64(retryMax), float64(retryMin)*math.Pow(2, exponent))
	jittered := time.Duration(delay * (0.75 + rand.Float64()*0.5))
	c.timer = time.AfterFunc(jittered, func() { c.connect(context.Background()) })
	c.mu.Unlock()
}

// waitReady waits until the host accepted the login, or times out.
func (c *connector) waitReady(timeout time.Duration) bool {
	c.mu.Lock()
	if c.ready {
		c.mu.Unlock()
		return true
	}
	if c.stopped {
		c.mu.Unlock()
		return false
	}
	waiter := make(chan bool, 1)
	c.waiters = append(c.waiters, waiter)
	c.mu.Unlock()
	select {
	case value := <-waiter:
		return value
	case <-time.After(timeout):
		return false
	}
}

// stop ends the connection for good (the person left the pool).
func (c *connector) stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	c.ready = false
	ws := c.ws
	c.ws = nil
	waiters := c.waiters
	c.waiters = nil
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()
	c.endpoint.closeAll("Left the pool.")
	if ws != nil {
		_ = ws.close(int(websocket.StatusNormalClosure), "Left the pool")
	}
	for _, waiter := range waiters {
		select {
		case waiter <- false:
		default:
		}
	}
}

// connectorKey is the map key of one connection.
func connectorKey(userID, poolID string) string { return userID + "|" + poolID }

// ensure keeps a connection to this pool's host while we are a member of it and
// the host relays; it ends it otherwise.
func (s *Service) ensureRelay(ctx context.Context, userID, poolID string) *connector {
	key := connectorKey(userID, poolID)
	owner, err := s.pool(ctx, userID, poolID)
	if err != nil {
		s.log().Warn("Could not read a pool while opening its relay", "error", err)
		return nil
	}
	wanted := owner != nil && !owner.IsHost && owner.HostRelay
	s.relays.mu.Lock()
	existing := s.relays.byKey[key]
	if existing != nil && (!wanted || existing.hostURL != owner.HostURL) {
		s.relays.mu.Unlock()
		existing.stop()
		s.relays.mu.Lock()
		delete(s.relays.byKey, key)
	}
	if !wanted || s.relays.byKey[key] != nil {
		found := s.relays.byKey[key]
		s.relays.mu.Unlock()
		return found
	}
	connection := &connector{
		service: s, userID: userID, poolID: poolID,
		hostURL: owner.HostURL, hostMemberID: owner.HostMemberID,
		checkHost: s.assertReachable, onUnknown: func(ctx context.Context, memberID string) error {
			return s.lookUpSender(ctx, userID, poolID, memberID)
		},
		online: map[string]bool{},
	}
	connection.endpoint = newRelayEndpoint(s, userID, poolID, connection.send,
		func(ctx context.Context, memberID string) error { return s.lookUpSender(ctx, userID, poolID, memberID) })
	s.relays.byKey[key] = connection
	s.relays.mu.Unlock()
	go connection.connect(context.Background())
	return connection
}

// stopRelay ends one connection (the person left the pool).
func (s *Service) stopRelay(userID, poolID string) {
	key := connectorKey(userID, poolID)
	s.relays.mu.Lock()
	connection := s.relays.byKey[key]
	delete(s.relays.byKey, key)
	s.relays.mu.Unlock()
	if connection != nil {
		connection.stop()
	}
}

// stopAll ends every connection.
func (c *connectorSet) stopAll() {
	c.mu.Lock()
	all := make([]*connector, 0, len(c.byKey))
	for _, connection := range c.byKey {
		all = append(all, connection)
	}
	c.byKey = map[string]*connector{}
	c.mu.Unlock()
	for _, connection := range all {
		connection.stop()
	}
}

// connectorFor is the connection to one pool's host, nil when there is none.
func (s *Service) connectorFor(userID, poolID string) *connector {
	s.relays.mu.Lock()
	defer s.relays.mu.Unlock()
	return s.relays.byKey[connectorKey(userID, poolID)]
}

// connectorEndpointFor is the endpoint to ask partners through, while connected.
func (s *Service) connectorEndpointFor(userID, poolID string) *relayEndpoint {
	connection := s.connectorFor(userID, poolID)
	if connection == nil {
		return nil
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if !connection.ready {
		return nil
	}
	return connection.endpoint
}

// waitRelayReady waits for the connection to one pool's host.
func (s *Service) waitRelayReady(userID, poolID string, timeout time.Duration) bool {
	connection := s.connectorFor(userID, poolID)
	if connection == nil {
		return false
	}
	return connection.waitReady(timeout)
}

// connectorIsOnline reports true/false when the host told us, and false when we
// do not know (not connected).
func (s *Service) connectorIsOnline(userID, poolID, memberID string) (bool, bool) {
	connection := s.connectorFor(userID, poolID)
	if connection == nil {
		return false, false
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if !connection.ready {
		return false, false
	}
	return connection.online[memberID], true
}

// connectorConnected reports whether the connection to one pool's host is up.
func (s *Service) connectorConnected(userID, poolID string) bool {
	connection := s.connectorFor(userID, poolID)
	if connection == nil {
		return false
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return connection.ready
}
