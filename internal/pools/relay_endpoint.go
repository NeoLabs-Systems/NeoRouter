package pools

// One person's end of the relay in one pool (relay/endpoint.js). It asks partners
// (request) and answers them (incoming 'open' frames), over a channel that is
// either a WebSocket to the host (relay_connector.go) or, on the host itself, the
// hub (relay_hub.go). Everything it sends is sealed for the one partner
// (relay_seal.go).
//
// Answering is deliberately narrow: only the calls a partner can make over the
// network anyway (models, chat completions and the pool messages addressed to a
// member) are handed to this NeoRouter (relay_loopback.go), with a short list of
// headers. Anything else is refused before it reaches the app.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// partnerCalls are the calls a relayed member answers.
var partnerCalls = []struct {
	method  string
	pattern *regexp.Regexp
}{
	{http.MethodGet, regexp.MustCompile(`^/v1/models(\?[\w=&.%-]*)?$`)},
	{http.MethodPost, regexp.MustCompile(`^/v1/chat/completions$`)},
	{http.MethodPost, regexp.MustCompile(`^/federation/v1/(introduce|leave|remove|dissolve|shared_changed)$`)},
}

// requestHeaders are the headers a request keeps on its way to the partner.
var requestHeaders = map[string]bool{
	"authorization": true, "content-type": true, "accept": true, "user-agent": true, "x-neorouter-hop": true,
}

// responseHeaders are the headers an answer keeps on its way back.
var responseHeaders = regexp.MustCompile(`^(content-type|retry-after|x-ratelimit-[a-z-]+)$`)

// allowed reports whether a partner may make this call through the host.
func allowed(method, path string) bool {
	for _, call := range partnerCalls {
		if call.method == method && call.pattern.MatchString(path) {
			return true
		}
	}
	return false
}

// relayRequest is one request to send to a partner.
type relayRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    io.Reader
}

// relayEndpoint is one person's end of the relay in one pool.
type relayEndpoint struct {
	service   *Service
	userID    string
	poolID    string
	send      func(any) error
	onUnknown func(ctx context.Context, memberID string) error

	mu      sync.Mutex
	streams map[string]*endpointStream
	// answered remembers requests already answered, so a replay is refused.
	answered map[string]int64
}

// endpointStream is one stream this end takes part in.
type endpointStream struct {
	role  string // asking | answering
	frame func(relayFrame)
	close func(reason string)
}

// newRelayEndpoint builds one end. send delivers a frame to the hub and returns
// once it is written; onUnknown may refresh the member list once when a partner's
// sealing key is not known yet.
func newRelayEndpoint(service *Service, userID, poolID string, send func(any) error,
	onUnknown func(ctx context.Context, memberID string) error) *relayEndpoint {
	return &relayEndpoint{
		service: service, userID: userID, poolID: poolID, send: send, onUnknown: onUnknown,
		streams: map[string]*endpointStream{}, answered: map[string]int64{},
	}
}

// own is this person's half of the sealing key pair.
func (e *relayEndpoint) own(ctx context.Context) (sealingKey, error) {
	record, err := e.service.ensureIdentity(ctx, e.userID)
	if err != nil {
		return sealingKey{}, err
	}
	if record.BoxPrivateKey == nil || record.BoxKey == "" {
		return sealingKey{}, errors.New("pools: this person has no sealing key yet")
	}
	return sealingKey{PrivateKey: record.BoxPrivateKey, OwnBoxKey: record.BoxKey, OwnMemberID: record.MemberID}, nil
}

// relayDirections are the two directions of one stream.
type relayDirections struct {
	request *direction
	answer  func(salt string) (*direction, error)
}

// directions derives the request direction of a stream (asker → answerer, keyed
// by the stream id alone) and the answer direction (answerer → asker, also keyed
// by the answerer's fresh salt).
func (e *relayEndpoint) directions(ctx context.Context, partner *member, streamID string, asking bool) (relayDirections, error) {
	own, err := e.own(ctx)
	if err != nil {
		return relayDirections{}, err
	}
	pair, err := e.service.pairKey(own, sealPartner{MemberID: partner.MemberID, BoxKey: partner.BoxKey}, e.poolID)
	if err != nil {
		return relayDirections{}, err
	}
	asker, answerer := own.OwnMemberID, partner.MemberID
	if !asking {
		asker, answerer = partner.MemberID, own.OwnMemberID
	}
	request, err := directionFor(pair, e.poolID, streamID, asker, answerer, "")
	if err != nil {
		return relayDirections{}, err
	}
	return relayDirections{
		request: request,
		answer: func(salt string) (*direction, error) {
			return directionFor(pair, e.poolID, streamID, answerer, asker, salt)
		},
	}, nil
}

// firstTime remembers a request id, false when it was seen before (a replay).
func (e *relayEndpoint) firstTime(id string, now int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for key, at := range e.answered {
		if now-at > relayLimits.RequestSkewMS*2 {
			delete(e.answered, key)
		}
	}
	if _, seen := e.answered[id]; seen {
		return false
	}
	e.answered[id] = now
	return true
}

// partner is the member this end sends to, nil when their sealing key is unknown.
func (e *relayEndpoint) partner(ctx context.Context, memberID string) *member {
	member, err := e.service.member(ctx, e.userID, e.poolID, memberID)
	if err != nil || member == nil || member.BoxKey == "" {
		return nil
	}
	return member
}

// unreachableError is what a request that cannot reach its partner fails with.
func unreachableError(message, code string) error {
	return fail(http.StatusBadGateway, code, message)
}

// randomStreamID is 16 random bytes as 22 URL-safe characters, the shape a
// stream id has.
func randomStreamID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return strings.Repeat("0", 22)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// askingRequest is the stream of a request this end asks.
type askingRequest struct {
	keys            relayDirections
	headOnce        sync.Once
	head            chan *relayResponse
	failure         chan error
	body            *io.PipeWriter
	answerDirection *direction
	headSeen        bool
	mu              sync.Mutex
	cancel          func()
}

// request sends a request to a partner through the host and answers with the
// partner's response.
func (e *relayEndpoint) request(ctx context.Context, memberID string, req relayRequest) (*http.Response, error) {
	partner := e.partner(ctx, memberID)
	if partner == nil {
		return nil, unreachableError("This partner cannot be reached through the host yet.", "RELAY_UNREACHABLE")
	}
	var payload []byte
	if req.Body != nil {
		read, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("pools: read the relayed request: %w", err)
		}
		payload = read
	}
	if len(payload) > relayLimits.RequestBodyBytes {
		return nil, unreachableError("The request is too large to pass through the host.", "RELAY_TOO_LARGE")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	streamID := randomStreamID()
	keys, err := e.directions(ctx, partner, streamID, true)
	if err != nil {
		return nil, err
	}
	message, err := json.Marshal(struct {
		TS      int64             `json:"ts"`
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}{
		TS: e.service.nowMS(), Method: req.Method, Path: req.Path,
		Headers: pickHeaders(req.Headers, func(name string) bool { return requestHeaders[name] }),
		Body:    base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		return nil, fmt.Errorf("pools: encode the relayed request: %w", err)
	}
	asking := &askingRequest{
		keys:    keys,
		head:    make(chan *relayResponse, 1),
		failure: make(chan error, 1),
	}
	ctx, cancel := context.WithCancel(ctx)
	asking.cancel = cancel
	stream := &endpointStream{
		role:  "asking",
		frame: func(frame relayFrame) { asking.onFrame(ctx, frame) },
		close: func(reason string) { asking.finish(unreachableError(reason, "RELAY_UNREACHABLE")) },
	}
	e.mu.Lock()
	e.streams[streamID] = stream
	e.mu.Unlock()

	seq, sealed := keys.request.sealFrame("open", message)
	if err := e.send(streamFrame{T: "open", S: streamID, To: memberID, Seq: seq, D: sealed}); err != nil {
		e.dropStream(streamID)
		cancel()
		return nil, unreachableError("The host could not be reached: "+err.Error(), "RELAY_UNREACHABLE")
	}
	select {
	case response := <-asking.head:
		return &http.Response{
			Status:     fmt.Sprintf("%d %s", response.Status, http.StatusText(response.Status)),
			StatusCode: response.Status,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     response.Header,
			Body:       &relayResponseBody{ReadCloser: response.Body, endpoint: e, streamID: streamID, cancel: cancel},
		}, nil
	case err := <-asking.failure:
		cancel()
		return nil, err
	case <-ctx.Done():
		e.dropStream(streamID)
		cancel()
		_ = e.send(endFrame{T: "cancel", S: streamID})
		return nil, ctx.Err()
	}
}

// relayResponseBody carries one relayed answer; closing it ends the stream.
type relayResponseBody struct {
	io.ReadCloser
	endpoint *relayEndpoint
	streamID string
	cancel   func()
}

// Close ends the stream and tells the partner it can stop.
func (b *relayResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.endpoint.dropStream(b.streamID)
	b.cancel()
	_ = b.endpoint.send(endFrame{T: "cancel", S: b.streamID})
	return err
}

// onFrame handles one frame of a stream this end asked.
func (a *askingRequest) onFrame(ctx context.Context, frame relayFrame) {
	switch frame.typ() {
	case "fail":
		code, _ := frame.text("code")
		if code == "" {
			code = "failed"
		}
		a.finish(unreachableError("The partner could not be reached through the host ("+code+").", "RELAY_UNREACHABLE"))
	case "head":
		a.mu.Lock()
		seen := a.headSeen
		a.headSeen = true
		a.mu.Unlock()
		if seen {
			return
		}
		salt, _ := frame.text("r")
		answer, err := a.keys.answer(salt)
		if err != nil {
			a.finish(unreachableError("An answer through the host could not be verified: "+err.Error(), "RELAY_SEAL"))
			return
		}
		seq, _ := frame.integer("seq")
		sealed, _ := frame.text("d")
		plain, err := answer.openFrame("head", uint64(seq), sealed)
		if err != nil {
			a.finish(unreachableError("An answer through the host could not be verified: "+err.Error(), "RELAY_SEAL"))
			return
		}
		var head struct {
			Status  int               `json:"status"`
			Headers map[string]string `json:"headers"`
		}
		if err := json.Unmarshal(plain, &head); err != nil {
			a.finish(unreachableError("The partner sent an unexpected answer through the host.", "RELAY_UNREACHABLE"))
			return
		}
		status := head.Status
		if status == 0 {
			status = http.StatusBadGateway
		}
		reader, writer := io.Pipe()
		a.mu.Lock()
		a.body = writer
		a.answerDirection = answer
		a.mu.Unlock()
		a.head <- &relayResponse{Status: status, Header: headerOf(head.Headers), Body: reader}
	case "data":
		a.mu.Lock()
		answer, seen, body := a.answerDirection, a.headSeen, a.body
		a.mu.Unlock()
		if !seen || answer == nil || body == nil {
			a.finish(unreachableError("The partner sent an unexpected answer through the host.", "RELAY_UNREACHABLE"))
			return
		}
		seq, _ := frame.integer("seq")
		sealed, _ := frame.text("d")
		plain, err := answer.openFrame("data", uint64(seq), sealed)
		if err != nil {
			a.finish(unreachableError("An answer through the host could not be verified: "+err.Error(), "RELAY_SEAL"))
			return
		}
		if _, err := body.Write(plain); err != nil {
			a.finish(nil)
		}
	case "end":
		a.mu.Lock()
		answer := a.answerDirection
		a.mu.Unlock()
		seq, _ := frame.integer("seq")
		sealed, _ := frame.text("d")
		if answer != nil {
			if _, err := answer.openFrame("end", uint64(seq), sealed); err != nil {
				a.finish(unreachableError("An answer through the host could not be verified: "+err.Error(), "RELAY_SEAL"))
				return
			}
		}
		a.finish(nil)
	default:
		a.finish(unreachableError("The partner sent an unexpected answer through the host.", "RELAY_UNREACHABLE"))
	}
}

// finish ends the stream, with an error before the answer started and as the
// answer's end after.
func (a *askingRequest) finish(err error) {
	a.mu.Lock()
	body := a.body
	seen := a.headSeen
	a.mu.Unlock()
	a.headOnce.Do(func() {
		if seen && body != nil {
			if err != nil {
				_ = body.CloseWithError(err)
			} else {
				_ = body.Close()
			}
			return
		}
		if err != nil {
			select {
			case a.failure <- err:
			default:
			}
		}
	})
}

// dropStream forgets one stream.
func (e *relayEndpoint) dropStream(streamID string) {
	e.mu.Lock()
	delete(e.streams, streamID)
	e.mu.Unlock()
}

// registerStream remembers one stream, false when the id is already taken.
func (e *relayEndpoint) registerStream(streamID string, stream *endpointStream) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, taken := e.streams[streamID]; taken {
		return false
	}
	e.streams[streamID] = stream
	return true
}

// answerRequest is the sealed request one partner sent.
type answerRequest struct {
	TS      int64             `json:"ts"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// isClosed reports whether a cancel signal has fired.
func isClosed(signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	default:
		return false
	}
}

// answer answers one partner's request (the stream's slot is already taken, so
// the limit holds while we wait).
func (e *relayEndpoint) answer(ctx context.Context, frame relayFrame, cancelled <-chan struct{}) {
	streamID, _ := frame.text("s")
	from, _ := frame.text("from")
	refuse := func(code string) {
		_ = e.send(endFrame{T: "fail", S: streamID, Code: code})
	}
	partner := e.partner(ctx, from)
	if partner == nil && e.onUnknown != nil {
		if err := e.onUnknown(ctx, from); err == nil {
			partner = e.partner(ctx, from)
		}
	}
	if partner == nil {
		refuse("unknown_sender")
		return
	}
	keys, err := e.directions(ctx, partner, streamID, false)
	if err != nil {
		refuse("unreadable")
		return
	}
	seq, _ := frame.integer("seq")
	sealed, _ := frame.text("d")
	plain, err := keys.request.openFrame("open", uint64(seq), sealed)
	if err != nil {
		refuse("unreadable")
		return
	}
	var request answerRequest
	if err := json.Unmarshal(plain, &request); err != nil {
		refuse("unreadable")
		return
	}
	now := e.service.nowMS()
	// A request the host kept and sends again is refused: too old, or already seen.
	if skew := now - request.TS; skew > relayLimits.RequestSkewMS || skew < -relayLimits.RequestSkewMS {
		refuse("replayed")
		return
	}
	if !e.firstTime(partner.MemberID+"|"+streamID, now) {
		refuse("replayed")
		return
	}
	salt := randomStreamID()
	out, err := keys.answer(salt)
	if err != nil {
		refuse("failed")
		return
	}
	headSent := false
	var headMu sync.Mutex
	reply := func(frameType string, payload []byte) error {
		frameSeq, frameData := out.sealFrame(frameType, payload)
		out := streamFrame{T: frameType, S: streamID, Seq: frameSeq, D: frameData}
		headMu.Lock()
		if !headSent {
			out.R = salt
			headSent = true
		}
		headMu.Unlock()
		return e.send(out)
	}
	method := strings.ToUpper(request.Method)
	path := request.Path
	if !allowed(method, path) {
		_ = reply("head", []byte(`{"status":403,"headers":{"content-type":"application/json"}}`))
		_ = reply("data", []byte(`{"error":{"code":"POOL_RELAY_REFUSED","message":"This call is not passed through a pool host."}}`))
		_ = reply("end", []byte{})
		return
	}
	body, err := base64.StdEncoding.DecodeString(request.Body)
	if err != nil {
		refuse("unreadable")
		return
	}
	headers := http.Header{}
	for name, value := range request.Headers {
		if requestHeaders[strings.ToLower(name)] {
			headers.Set(name, value)
		}
	}
	headers.Set("content-length", itoa(int64(len(body))))
	response, err := e.service.loopbackDispatch(ctx, relayRequest{
		Method: method, Path: path, Headers: headers, Body: bytes.NewReader(body),
	}, cancelled)
	if err != nil {
		if !isClosed(cancelled) {
			refuse("failed")
		}
		return
	}
	defer response.Body.Close()
	head := struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
	}{
		Status:  response.Status,
		Headers: pickHeaders(response.Header, func(name string) bool { return responseHeaders.MatchString(name) }),
	}
	encoded, err := json.Marshal(head)
	if err != nil {
		refuse("failed")
		return
	}
	if err := reply("head", encoded); err != nil {
		return
	}
	chunk := make([]byte, relayLimits.ChunkBytes)
	for {
		read, readErr := response.Body.Read(chunk)
		if read > 0 {
			if err := reply("data", chunk[:read]); err != nil {
				return
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			if !isClosed(cancelled) {
				refuse("failed")
			}
			return
		}
	}
	_ = reply("end", []byte{})
}

// handleFrame handles one frame from the hub.
func (e *relayEndpoint) handleFrame(frame relayFrame) {
	if err := validStreamFrame(frame); err != nil {
		return
	}
	streamID, _ := frame.text("s")
	if frame.typ() == "open" {
		if _, ok := frame.text("from"); !ok {
			return
		}
		e.mu.Lock()
		_, taken := e.streams[streamID]
		count := len(e.streams)
		e.mu.Unlock()
		if taken {
			return
		}
		if count >= relayLimits.StreamsPerConnection {
			_ = e.send(endFrame{T: "fail", S: streamID, Code: "busy"})
			return
		}
		cancelled := make(chan struct{})
		var once sync.Once
		stop := func() { once.Do(func() { close(cancelled) }) }
		stream := &endpointStream{
			role: "answering",
			frame: func(next relayFrame) {
				if next.typ() == "cancel" {
					stop()
				}
			},
			close: func(string) { stop() },
		}
		e.registerStream(streamID, stream)
		go func() {
			defer e.dropStream(streamID)
			e.answer(context.Background(), frame, cancelled)
			stop()
		}()
		return
	}
	e.mu.Lock()
	stream := e.streams[streamID]
	e.mu.Unlock()
	if stream != nil && stream.frame != nil {
		stream.frame(frame)
	}
}

// closeAll ends every open stream (the connection went away).
func (e *relayEndpoint) closeAll(reason string) {
	e.mu.Lock()
	streams := make([]*endpointStream, 0, len(e.streams))
	for _, stream := range e.streams {
		streams = append(streams, stream)
	}
	e.streams = map[string]*endpointStream{}
	e.mu.Unlock()
	for _, stream := range streams {
		if stream.close != nil {
			stream.close(reason)
		}
	}
}

// itoa is a small helper for the numbers pool code writes.
func itoa(value int64) string { return strconv.FormatInt(value, 10) }
