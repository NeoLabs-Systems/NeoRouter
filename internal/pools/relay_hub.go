package pools

// The host's side of the relay: members without a public address keep a
// WebSocket open to /federation/v1/relay, and the hub passes stream frames
// between the two members of a stream. It reads only the routing fields; the
// requests and answers themselves are sealed between the members
// (relay_seal.go), so the host can neither read nor change them.
//
// Logging in: the hub sends a random challenge; the member signs it with its pool
// key (the one in the host's member list). Only current members of a pool this
// instance hosts, with relaying on, get in; a newer connection of the same member
// replaces the older one.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/coder/websocket"
)

// RelayPath is the address members keep a WebSocket open to.
const RelayPath = "/federation/v1/relay"

// failedLoginsPerMinute is how often one client address may fail to log in
// before it has to wait.
const failedLoginsPerMinute = 20

// close codes the relay uses (the 4000 range is the application's).
const (
	relayCloseReplaced     = 4000 // replaced by a newer connection
	relayCloseMalformed    = 4400 // not logged in, or not a frame
	relayCloseLoginTimeout = 4401 // no hello in time
	relayCloseNotMember    = 4403 // not a member of a pool relayed here
	relayCloseStopped      = 4410 // relaying stopped, or removed from the pool
)

// relayConn is one member's connection to the hub, or the host's own end.
type relayConn struct {
	poolID   string
	memberID string
	// ws is nil for the host's own end, which talks to its endpoint directly.
	ws *relaySocket
	// deliver passes one frame along.
	deliver func(any) error
	alive   bool
	streams map[string]bool
	mu      sync.Mutex
}

// send passes one frame along (the field is swapped by the tests, so it is read
// under the connection's lock).
func (c *relayConn) send(frame any) error {
	c.mu.Lock()
	deliver := c.deliver
	c.mu.Unlock()
	if deliver == nil {
		return nil
	}
	return deliver(frame)
}

// setDeliver replaces how frames reach this connection.
func (c *relayConn) setDeliver(deliver func(any) error) func(any) error {
	c.mu.Lock()
	previous := c.deliver
	c.deliver = deliver
	c.mu.Unlock()
	return previous
}

// relayStream is one stream the hub passes along.
type relayStream struct {
	asker    *relayConn
	answerer *relayConn
	at       int64
}

// relayHub is the host side of the relay.
type relayHub struct {
	service *Service

	mu             sync.Mutex
	connections    map[string]*relayConn
	streams        map[string]*relayStream
	localEndpoints map[string]*localRelayEndpoint
	failedLogins   map[string][]int64

	stopOnce sync.Once
	stop     chan struct{}
	stopped  bool
}

// localRelayEndpoint is the host's own end in one of its pools.
type localRelayEndpoint struct {
	endpoint *relayEndpoint
	conn     *relayConn
}

func newRelayHub(service *Service) *relayHub {
	return &relayHub{
		service:        service,
		connections:    map[string]*relayConn{},
		streams:        map[string]*relayStream{},
		localEndpoints: map[string]*localRelayEndpoint{},
		failedLogins:   map[string][]int64{},
		stop:           make(chan struct{}),
	}
}

// connectionKey is the map key of one member's connection.
func connectionKey(poolID, memberID string) string { return poolID + "|" + memberID }

// start begins the heartbeat sweep.
func (h *relayHub) start() {
	go func() {
		ticker := time.NewTicker(time.Duration(relayLimits.HeartbeatMS) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-ticker.C:
				h.sweep()
			}
		}
	}()
}

// stopHub ends every connection and the sweep.
func (h *relayHub) stopHub() {
	h.stopOnce.Do(func() { close(h.stop) })
	h.mu.Lock()
	h.stopped = true
	conns := make([]*relayConn, 0, len(h.connections))
	for _, conn := range h.connections {
		conns = append(conns, conn)
	}
	h.connections = map[string]*relayConn{}
	locals := make([]*localRelayEndpoint, 0, len(h.localEndpoints))
	for _, local := range h.localEndpoints {
		locals = append(locals, local)
	}
	h.localEndpoints = map[string]*localRelayEndpoint{}
	h.streams = map[string]*relayStream{}
	h.mu.Unlock()
	for _, conn := range conns {
		if conn.ws != nil {
			_ = conn.ws.close(int(websocket.StatusGoingAway), "Relaying stopped")
		}
	}
	for _, local := range locals {
		local.endpoint.closeAll("Relaying stopped for this pool.")
	}
}

// online lists the member ids connected in one pool.
func (h *relayHub) online(poolID string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := []string{}
	for _, conn := range h.connections {
		if conn.poolID == poolID {
			list = append(list, conn.memberID)
		}
	}
	return list
}

// isOnline reports whether one member of one pool is connected.
func (h *relayHub) isOnline(poolID, memberID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.connections[connectionKey(poolID, memberID)]
	return ok
}

// endStream ends one stream and tells the members it names.
func (h *relayHub) endStream(id string, notify [][2]any) {
	h.mu.Lock()
	stream, ok := h.streams[id]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(h.streams, id)
	delete(stream.asker.streams, id)
	delete(stream.answerer.streams, id)
	h.mu.Unlock()
	for _, item := range notify {
		conn, _ := item[0].(*relayConn)
		if conn == nil {
			continue
		}
		_ = conn.send(item[1])
	}
}

// route passes one frame along, or ends the stream it belongs to.
func (h *relayHub) route(from *relayConn, frame relayFrame) {
	if err := validStreamFrame(frame); err != nil {
		return
	}
	streamID, _ := frame.text("s")
	if frame.typ() == "open" {
		to, _ := frame.text("to")
		h.mu.Lock()
		answerer := h.connections[connectionKey(from.poolID, to)]
		_, taken := h.streams[streamID]
		tooMany := len(from.streams) >= relayLimits.StreamsPerConnection
		h.mu.Unlock()
		if taken || tooMany {
			_ = from.send(endFrame{T: "fail", S: streamID, Code: "busy"})
			return
		}
		if answerer == nil || answerer == from {
			_ = from.send(endFrame{T: "fail", S: streamID, Code: "unreachable"})
			return
		}
		h.mu.Lock()
		h.streams[streamID] = &relayStream{asker: from, answerer: answerer, at: h.service.nowMS()}
		from.streams[streamID] = true
		answerer.streams[streamID] = true
		h.mu.Unlock()
		seq, _ := frame.integer("seq")
		sealed, _ := frame.text("d")
		_ = answerer.send(streamFrame{T: "open", S: streamID, From: from.memberID, Seq: uint64(seq), D: sealed})
		return
	}
	h.mu.Lock()
	stream, ok := h.streams[streamID]
	if !ok || (stream.asker != from && stream.answerer != from) {
		h.mu.Unlock()
		return
	}
	stream.at = h.service.nowMS()
	other := stream.asker
	if other == from {
		other = stream.answerer
	}
	h.mu.Unlock()
	forwarded, err := forwardedFrame(frame)
	if err != nil {
		return
	}
	switch frame.typ() {
	case "end", "cancel", "fail":
		h.endStream(streamID, [][2]any{{other, forwarded}})
		return
	}
	// A member that does not read its answers would make the host buffer without
	// end.
	if other.ws != nil && other.ws.buffered() > relayLimits.BufferedBytes {
		h.endStream(streamID, [][2]any{
			{other, endFrame{T: "fail", S: streamID, Code: "slow"}},
			{from, endFrame{T: "cancel", S: streamID}},
		})
		return
	}
	_ = other.send(forwarded)
}

// dropStreams tells the other side of each of a connection's streams that it is
// gone.
func (h *relayHub) dropStreams(conn *relayConn) {
	conn.mu.Lock()
	ids := make([]string, 0, len(conn.streams))
	for id := range conn.streams {
		ids = append(ids, id)
	}
	conn.mu.Unlock()
	for _, id := range ids {
		h.mu.Lock()
		stream, ok := h.streams[id]
		h.mu.Unlock()
		if !ok {
			continue
		}
		if stream.asker == conn {
			h.endStream(id, [][2]any{{stream.answerer, endFrame{T: "cancel", S: id}}})
		} else {
			h.endStream(id, [][2]any{{stream.asker, endFrame{T: "fail", S: id, Code: "unreachable"}}})
		}
	}
}

// broadcastPresence tells a pool's members that one of them came or went.
func (h *relayHub) broadcastPresence(poolID, memberID string, up bool) {
	h.mu.Lock()
	conns := make([]*relayConn, 0, len(h.connections))
	for _, conn := range h.connections {
		if conn.poolID == poolID && conn.memberID != memberID {
			conns = append(conns, conn)
		}
	}
	h.mu.Unlock()
	for _, conn := range conns {
		if conn.ws != nil {
			_ = conn.send(presenceFrame{T: "presence", Member: memberID, Online: up})
		}
	}
	h.service.onPresence(presenceEvent{PoolID: poolID, MemberID: memberID, Online: up})
}

// register adds one member's connection, replacing an older one.
func (h *relayHub) register(conn *relayConn) {
	key := connectionKey(conn.poolID, conn.memberID)
	h.mu.Lock()
	previous := h.connections[key]
	h.connections[key] = conn
	h.mu.Unlock()
	if previous != nil {
		h.mu.Lock()
		delete(h.connections, key)
		h.mu.Unlock()
		h.dropStreams(previous)
		if previous.ws != nil {
			_ = previous.ws.close(relayCloseReplaced, "Replaced by a newer connection")
		}
		h.mu.Lock()
		h.connections[key] = conn
		h.mu.Unlock()
	}
	ready := []string{}
	for _, id := range h.online(conn.poolID) {
		if id != conn.memberID {
			ready = append(ready, id)
		}
	}
	_ = conn.send(readyFrame{T: "ready", Online: ready})
	h.broadcastPresence(conn.poolID, conn.memberID, true)
}

// unregister removes one member's connection.
func (h *relayHub) unregister(conn *relayConn) {
	key := connectionKey(conn.poolID, conn.memberID)
	h.mu.Lock()
	current, ok := h.connections[key]
	if !ok || current != conn {
		h.mu.Unlock()
		return
	}
	delete(h.connections, key)
	h.mu.Unlock()
	h.dropStreams(conn)
	h.broadcastPresence(conn.poolID, conn.memberID, false)
}

// login checks a hello frame and returns the connection it authorises.
func (h *relayHub) login(ctx context.Context, ws *relaySocket, nonce string, frame relayFrame) (*relayConn, error) {
	if frame.typ() != "hello" {
		return nil, errors.New("not a hello")
	}
	poolID, _ := frame.text("pool")
	memberID, _ := frame.text("member")
	signature, _ := frame.text("sig")
	hosted, err := h.service.hostedPool(ctx, poolID)
	if err != nil || hosted == nil || !hosted.RelayEnabled {
		return nil, errors.New("no such pool")
	}
	member, err := h.service.member(ctx, hosted.UserID, hosted.PoolID, memberID)
	if err != nil || member == nil || member.MemberID == hosted.HostMemberID {
		return nil, errors.New("not a member")
	}
	if !verifySignature(member.PublicKey, helloText(nonce, hosted.PoolID, member.MemberID, hosted.HostMemberID), signature) {
		return nil, errors.New("bad signature")
	}
	conn := &relayConn{
		poolID: hosted.PoolID, memberID: member.MemberID, ws: ws,
		alive: true, streams: map[string]bool{},
	}
	conn.setDeliver(func(frame any) error { return ws.send(encodeFrame(frame)) })
	return conn, nil
}

// recentFailures counts a client's failed logins in the last minute.
func (h *relayHub) recentFailures(client string, now int64) []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	times := h.failedLogins[client]
	kept := make([]int64, 0, len(times))
	for _, at := range times {
		if now-at < 60_000 {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(h.failedLogins, client)
		return kept
	}
	h.failedLogins[client] = kept
	return kept
}

// noteFailedLogin records one failed login.
func (h *relayHub) noteFailedLogin(client string, now int64) {
	h.mu.Lock()
	if len(h.failedLogins) > 10_000 {
		for key, times := range h.failedLogins {
			stale := true
			for _, at := range times {
				if now-at < 60_000 {
					stale = false
					break
				}
			}
			if stale {
				delete(h.failedLogins, key)
			}
		}
	}
	times := h.failedLogins[client]
	kept := make([]int64, 0, len(times)+1)
	for _, at := range times {
		if now-at < 60_000 {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now)
	h.failedLogins[client] = kept
	h.mu.Unlock()
}

// mayTryLogin reports whether a client address may try to log in: only clients
// that keep failing wait; members logging in are never held up.
func (h *relayHub) mayTryLogin(client string, now int64) bool {
	return len(h.recentFailures(client, now)) < failedLoginsPerMinute
}

// sweep finds connections that died without closing; old streams end.
func (h *relayHub) sweep() {
	h.mu.Lock()
	conns := make([]*relayConn, 0, len(h.connections))
	for _, conn := range h.connections {
		conns = append(conns, conn)
	}
	h.mu.Unlock()
	for _, conn := range conns {
		if conn.ws == nil {
			continue
		}
		if !conn.alive {
			conn.ws.terminate()
			continue
		}
		conn.alive = false
		conn.ws.ping()
	}
	now := h.service.nowMS()
	h.mu.Lock()
	idle := []string{}
	for id, stream := range h.streams {
		if now-stream.at > relayLimits.StreamIdleMS {
			idle = append(idle, id)
		}
	}
	h.mu.Unlock()
	for _, id := range idle {
		h.mu.Lock()
		stream, ok := h.streams[id]
		h.mu.Unlock()
		if !ok {
			continue
		}
		h.endStream(id, [][2]any{
			{stream.asker, endFrame{T: "fail", S: id, Code: "timeout"}},
			{stream.answerer, endFrame{T: "cancel", S: id}},
		})
	}
}

// clientOf is the client's address as the rest of the app sees it (behind a
// proxy when trusted).
func (s *Service) clientOf(r *http.Request) string {
	trustProxy := false
	if s.Cfg != nil {
		trustProxy = s.Cfg().TrustProxy
	}
	return httpx.ClientIP(r, trustProxy)
}

// handler answers the relay path: the upgrade, the challenge and then the
// member's frames.
func (h *relayHub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isWebSocketUpgrade(r) {
			httpserver.NotFound(w)
			return
		}
		client := h.service.clientOf(r)
		if !h.mayTryLogin(client, h.service.nowMS()) {
			w.Header().Set("Connection", "close")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		ws, err := acceptRelaySocket(w, r)
		if err != nil {
			h.service.log().Debug("Could not accept a relay connection", "error", err)
			return
		}
		h.accept(ws, client)
	})
}

// isWebSocketUpgrade reports whether a request asks for an upgrade.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// accept is one member's connection: the challenge, the hello, then the frames.
func (h *relayHub) accept(ws *relaySocket, client string) {
	ctx := context.Background()
	nonceRaw := make([]byte, 24)
	_, _ = rand.Read(nonceRaw)
	nonce := base64.RawURLEncoding.EncodeToString(nonceRaw)

	var (
		conn *relayConn
		mu   sync.Mutex
	)
	timer := time.AfterFunc(time.Duration(relayLimits.HelloMS)*time.Millisecond, func() {
		_ = ws.close(relayCloseLoginTimeout, "Log in first")
	})
	defer timer.Stop()
	_ = ws.send(encodeFrame(challengeFrame{T: "challenge", Nonce: nonce}))

	for {
		messageType, data, err := ws.read(ctx)
		if err != nil {
			break
		}
		mu.Lock()
		current := conn
		mu.Unlock()
		if current == nil && len(data) > relayLimits.HelloBytes {
			_ = ws.close(relayCloseMalformed, "Log in first")
			break
		}
		if messageType != websocket.MessageText {
			_ = ws.close(relayCloseMalformed, "Malformed frame")
			break
		}
		frame, ok := parseRelayFrame(string(data))
		if !ok {
			_ = ws.close(relayCloseMalformed, "Malformed frame")
			break
		}
		if current == nil {
			loggedIn, loginErr := h.login(ctx, ws, nonce, frame)
			timer.Stop()
			if loginErr != nil || loggedIn == nil {
				h.noteFailedLogin(client, h.service.nowMS())
				_ = ws.close(relayCloseNotMember, "Not a member of a pool relayed here")
				break
			}
			mu.Lock()
			conn = loggedIn
			mu.Unlock()
			h.register(loggedIn)
			continue
		}
		h.route(current, frame)
	}
	mu.Lock()
	current := conn
	mu.Unlock()
	if current != nil {
		h.unregister(current)
	}
	ws.terminate()
}

// localEndpoint is the host's own end in one of its pools: it asks relayed
// members through the hub directly, without a WebSocket.
func (h *relayHub) localEndpoint(ctx context.Context, userID, poolID string) (*relayEndpoint, error) {
	key := userID + "|" + poolID
	h.mu.Lock()
	if existing, ok := h.localEndpoints[key]; ok {
		h.mu.Unlock()
		return existing.endpoint, nil
	}
	h.mu.Unlock()
	record, err := h.service.ensureIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}
	conn := &relayConn{poolID: poolID, memberID: record.MemberID, streams: map[string]bool{}}
	var endpoint *relayEndpoint
	endpoint = newRelayEndpoint(h.service, userID, poolID, func(frame any) error {
		h.routeLocal(conn, endpoint, frame)
		return nil
	}, func(ctx context.Context, memberID string) error {
		return h.service.lookUpSender(ctx, userID, poolID, memberID)
	})
	conn.setDeliver(func(frame any) error {
		if parsed, ok := frame.(relayFrame); ok {
			endpoint.handleFrame(parsed)
			return nil
		}
		encoded := encodeFrame(frame)
		parsed, ok := parseRelayFrame(encoded)
		if !ok {
			return fmt.Errorf("pool relay: unreadable frame")
		}
		endpoint.handleFrame(parsed)
		return nil
	})
	h.mu.Lock()
	h.localEndpoints[key] = &localRelayEndpoint{endpoint: endpoint, conn: conn}
	h.mu.Unlock()
	return endpoint, nil
}

// routeLocal passes one frame from the host's own end through the hub.
func (h *relayHub) routeLocal(conn *relayConn, endpoint *relayEndpoint, frame any) {
	encoded := encodeFrame(frame)
	parsed, ok := parseRelayFrame(encoded)
	if !ok {
		return
	}
	h.route(conn, parsed)
}

// connectionOf is one member's connection, for the tests.
func (h *relayHub) connectionOf(poolID, memberID string) *relayConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connections[connectionKey(poolID, memberID)]
}

// localEndOf is the host's own end in one pool, for the tests (forTests().
// localEndpoints).
func (h *relayHub) localEndOf(userID, poolID string) *localRelayEndpoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.localEndpoints[userID+"|"+poolID]
}

// closePool ends every connection of a pool (relaying turned off, pool closed).
func (h *relayHub) closePool(poolID string) {
	h.mu.Lock()
	conns := []*relayConn{}
	for _, conn := range h.connections {
		if conn.poolID == poolID {
			conns = append(conns, conn)
		}
	}
	locals := [][2]any{}
	for key, local := range h.localEndpoints {
		if local.conn.poolID == poolID {
			locals = append(locals, [2]any{key, local})
			delete(h.localEndpoints, key)
		}
	}
	h.mu.Unlock()
	for _, conn := range conns {
		if conn.ws != nil {
			_ = conn.ws.close(relayCloseStopped, "Relaying stopped for this pool")
		}
	}
	for _, item := range locals {
		local, _ := item[1].(*localRelayEndpoint)
		if local == nil {
			continue
		}
		h.dropStreams(local.conn)
		local.endpoint.closeAll("Relaying stopped for this pool.")
	}
}

// closeMember ends one member's connection (removed from the pool).
func (h *relayHub) closeMember(poolID, memberID string) {
	h.mu.Lock()
	conn, ok := h.connections[connectionKey(poolID, memberID)]
	h.mu.Unlock()
	if ok && conn.ws != nil {
		_ = conn.ws.close(relayCloseStopped, "No longer in this pool")
	}
}

// relaySocket is one WebSocket with a bounded outgoing queue, so a member that
// does not read cannot make the host buffer without end.
type relaySocket struct {
	ws *websocket.Conn

	mu     sync.Mutex
	queue  []string
	queued int
	closed bool
	wake   chan struct{}
	done   chan struct{}
}

// relayWriteTimeout bounds one write to a member.
const relayWriteTimeout = 30 * time.Second

// acceptRelaySocket upgrades one request.
func acceptRelaySocket(w http.ResponseWriter, r *http.Request) (*relaySocket, error) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // the relay authenticates with a signed hello
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(int64(relayLimits.FrameBytes))
	socket := &relaySocket{ws: ws, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go socket.writer()
	return socket, nil
}

// send queues one frame.
func (s *relaySocket) send(text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("pool relay: connection closed")
	}
	s.queue = append(s.queue, text)
	s.queued += len(text)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

// buffered is how many bytes are waiting to be written.
func (s *relaySocket) buffered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queued
}

// writer writes queued frames in order.
func (s *relaySocket) writer() {
	for {
		select {
		case <-s.done:
			return
		case <-s.wake:
			for {
				s.mu.Lock()
				if len(s.queue) == 0 {
					s.mu.Unlock()
					break
				}
				text := s.queue[0]
				s.queue = s.queue[1:]
				s.queued -= len(text)
				s.mu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), relayWriteTimeout)
				err := s.ws.Write(ctx, websocket.MessageText, []byte(text))
				cancel()
				if err != nil {
					s.terminate()
					return
				}
			}
		}
	}
}

// read reads one message.
func (s *relaySocket) read(ctx context.Context) (websocket.MessageType, []byte, error) {
	return s.ws.Read(ctx)
}

// ping sends a heartbeat.
func (s *relaySocket) ping() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), relayWriteTimeout)
		defer cancel()
		_ = s.ws.Ping(ctx)
	}()
}

// close closes the connection with a relay close code.
func (s *relaySocket) close(code int, reason string) error {
	return s.ws.Close(websocket.StatusCode(code), reason)
}

// terminate drops the connection without a goodbye.
func (s *relaySocket) terminate() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.queue = nil
	s.queued = 0
	close(s.done)
	s.mu.Unlock()
	_ = s.ws.CloseNow()
}
