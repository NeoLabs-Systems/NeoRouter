package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/coder/websocket"
)

const (
	// maxAttempts is how many providers one session may try.
	maxAttempts = 3
	// openTimeout bounds a provider's handshake.
	openTimeout = 15 * time.Second
	// maxPayload bounds one message either way (ws maxPayload).
	maxPayload = 16 << 20
	// writeTimeout bounds one write to either socket. The Node gateway relies on
	// the kernel's buffers; a session that cannot be written to is ended instead
	// of being kept forever.
	writeTimeout = 10 * time.Second
	// closeReasonLimit is the 120 bytes the Node gateway keeps of a close reason.
	closeReasonLimit = 120
	// privateDestinationText is the providers/net_guard.js refusal.
	privateDestinationText = "This address is on a local or private network. Only the instance admin can use those."
)

// firstMessageTimeout is how long the opening message may take to arrive. It is a
// variable only so a test can shorten the wait.
var firstMessageTimeout = 30 * time.Second

// pendingMessage is one client or provider message.
type pendingMessage struct {
	binary bool
	data   []byte
}

// upstream is one provider socket. The provider may speak right after the
// handshake, before the session is wired up; those messages wait here and are
// replayed by attach, so a provider that hangs up right away can be replaced by
// the next one.
type upstream struct {
	conn *websocket.Conn

	mu     sync.Mutex
	relay  func(binary bool, data []byte)
	early  []pendingMessage
	closed bool
	code   int
	reason string
}

// acceptClient completes the client's WebSocket handshake.
func acceptClient(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The router key authenticates the session; a browser's origin says
		// nothing about who it is (the Node gateway verifies no origin either).
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
		// Never echo a subprotocol that carries the key.
		Subprotocols: chooseSubprotocol(r),
	})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(maxPayload)
	return conn, nil
}

// chooseSubprotocol is the server's handleProtocols: the first protocol the
// client offered that does not carry the key.
func chooseSubprotocol(r *http.Request) []string {
	for _, protocol := range headerTokens(r, "Sec-WebSocket-Protocol") {
		if strings.HasPrefix(protocol, insecureKeyProtocol) {
			continue
		}
		return []string{protocol}
	}
	return nil
}

// openUpstream opens the provider's socket (openUpstream). A connection whose
// owner is not the instance admin may only reach public hosts.
func openUpstream(target upstreamTarget, publicOnly bool, resolve providers.Resolver) (*upstream, error) {
	if publicOnly {
		httpURL := httpFromWS(target.url)
		if providers.IsPrivateDestination(context.Background(), httpURL, resolve) {
			return nil, providers.NewError(providers.KindBadRequest, privateDestinationText, providers.ErrorOptions{})
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, target.url, &websocket.DialOptions{HTTPHeader: target.headers})
	if err != nil {
		if response != nil {
			return nil, classifyHandshake(response)
		}
		return nil, providers.FromNetwork(err)
	}
	conn.SetReadLimit(maxPayload)
	return &upstream{conn: conn, code: -1}, nil
}

// classifyHandshake is classifyHttpError(status, body, headers) for a refused
// upgrade: the body is parsed like the Node code's JSON.parse-or-string.
func classifyHandshake(response *http.Response) *providers.ProviderError {
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	var parsed any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &parsed); err != nil {
			parsed = string(body)
		}
	} else {
		parsed = ""
	}
	return providers.ClassifyHTTP(response.StatusCode, parsed, response.Header)
}

// httpFromWS is url.replace(/^ws/, 'http') for the private-host guard.
func httpFromWS(rawURL string) string {
	lowered := strings.ToLower(rawURL)
	switch {
	case strings.HasPrefix(lowered, "wss:"):
		return "https:" + rawURL[len("wss:"):]
	case strings.HasPrefix(lowered, "ws:"):
		return "http:" + rawURL[len("ws:"):]
	}
	return rawURL
}

// deliver hands one provider message to the session once it is attached, and
// holds it before that. Holding the lock across the relay keeps the provider's
// message order.
func (u *upstream) deliver(binary bool, data []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.relay == nil {
		u.early = append(u.early, pendingMessage{binary: binary, data: data})
		return
	}
	u.relay(binary, data)
}

// attach wires the session to the socket and replays what the provider said
// before it was wired.
func (u *upstream) attach(relay func(binary bool, data []byte)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.relay = relay
	held := u.early
	u.early = nil
	for _, message := range held {
		relay(message.binary, message.data)
	}
}

// noteClosed records how the socket ended.
func (u *upstream) noteClosed(code int, reason string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.closed = true
	u.code, u.reason = code, reason
}

// closedState is whether the socket has ended, and how.
func (u *upstream) closedState() (bool, int, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed, u.code, u.reason
}

// write sends one message to the provider.
func (u *upstream) write(binary bool, data []byte) error {
	kind := websocket.MessageText
	if binary {
		kind = websocket.MessageBinary
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return u.conn.Write(ctx, kind, data)
}

// writeJSON sends one message the relay rewrote.
func (u *upstream) writeJSON(value *object) error {
	encoded, err := marshalObject(value)
	if err != nil {
		return err
	}
	return u.write(false, encoded)
}

// close ends the socket without a closing handshake.
func (u *upstream) close() { _ = u.conn.CloseNow() }

// closeError splits a read error into the close code and reason the provider
// sent, -1 when it sent no close frame (Node reports 1006 then).
func closeError(err error) (int, string) {
	var closed websocket.CloseError
	if errors.As(err, &closed) {
		return int(closed.Code), closed.Reason
	}
	return -1, ""
}

// writeTimeoutContext bounds one client write.
func writeTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), writeTimeout)
}

// marshalObject encodes an ordered object the way JSON.stringify does (no HTML
// escaping, no trailing newline).
func marshalObject(value *object) ([]byte, error) {
	var buf strings.Builder
	encoder := jsonEncoder(&buf)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(buf.String(), "\n")), nil
}

// jsonEncoder is a JSON encoder that leaves HTML characters alone, as
// JSON.stringify does.
func jsonEncoder(buf *strings.Builder) *json.Encoder {
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	return encoder
}
