package proxies

// Outbound transport. Every proxy gets its own HTTP transport whose dialer
// opens the tunnel, so connections are reused, time out and are evicted per
// proxy.

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/proxy"
)

// ConnectError marks a failure that happened while establishing the tunnel. The
// request body was not sent yet, so retrying is safe.
type ConnectError struct{ Err error }

func (e *ConnectError) Error() string { return ErrConnect.Error() }
func (e *ConnectError) Unwrap() error { return e.Err }

// dispatcher is the retained transport for one proxy.
type dispatcher struct {
	usedAt    time.Time
	client    *http.Client
	transport *http.Transport
}

// close releases the proxy's idle connections. In-flight streams are not
// aborted: Go's http.Transport cannot be told to do that.
func (d *dispatcher) close(graceful bool) {
	if d == nil || d.transport == nil {
		return
	}
	_ = graceful
	d.transport.CloseIdleConnections()
}

// newDispatcher builds the transport for one proxy row.
func (s *Service) newDispatcher(row proxyRow) (*dispatcher, error) {
	plain, err := s.Sealer.Open(row.SecretEncrypted)
	if err != nil {
		return nil, fmt.Errorf("proxies: open proxy %s: %w", row.ID, err)
	}
	target, err := parseProxyURL(plain)
	if err != nil {
		return nil, fmt.Errorf("proxies: parse proxy %s: %w", row.ID, err)
	}
	p := s.pol
	connect := p.connectDuration()

	transport := &http.Transport{
		Proxy: nil, // the pool is the only proxy, and it must not consult the environment
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			began := time.Now()
			conn, err := dialThroughProxy(ctx, target, address, connect)
			if err != nil {
				return nil, err
			}
			s.recordLatency(row.ID, float64(time.Since(began).Microseconds())/1000)
			return conn, nil
		},
		TLSHandshakeTimeout:   connect,
		ResponseHeaderTimeout: 0, // streams may think for a long time before the first byte
		ExpectContinueTimeout: time.Second,
		MaxIdleConnsPerHost:   p.Connections,
		MaxConnsPerHost:       p.Connections,
		IdleConnTimeout:       p.idleDuration(),
		ForceAttemptHTTP2:     false, // HTTP/1.1 is spoken through a proxy; SSE and CONNECT both rely on it
	}
	return &dispatcher{
		usedAt:    time.Now(),
		client:    &http.Client{Transport: transport},
		transport: transport,
	}, nil
}

// dialThroughProxy opens a connection to destAddr ("host:port") through target.
func dialThroughProxy(ctx context.Context, target ProxyURL, destAddr string, timeout time.Duration) (net.Conn, error) {
	if target.Scheme == "http" || target.Scheme == "https" {
		return httpConnect(ctx, target, destAddr, timeout)
	}
	user, err := target.DecodedUser()
	if err != nil {
		return nil, &ConnectError{Err: err}
	}
	pass, err := target.DecodedPass()
	if err != nil {
		return nil, &ConnectError{Err: err}
	}
	var auth *proxy.Auth
	if user != "" || pass != "" {
		auth = &proxy.Auth{User: user, Password: pass}
	}
	dialer, err := proxy.SOCKS5("tcp", target.DialAddr(), auth, &net.Dialer{Timeout: timeout})
	if err != nil {
		return nil, &ConnectError{Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dialer.(proxy.ContextDialer).DialContext(ctx, "tcp", destAddr)
	if err != nil {
		return nil, &ConnectError{Err: err}
	}
	return conn, nil
}

// httpConnect performs an HTTP CONNECT tunnel through an http(s) proxy, sending
// the proxy credentials in Proxy-Authorization.
func httpConnect(ctx context.Context, target ProxyURL, destAddr string, timeout time.Duration) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", target.DialAddr())
	if err != nil {
		return nil, &ConnectError{Err: err}
	}
	if target.Scheme == "https" {
		host := target.Hostname
		if strings.HasPrefix(host, "[") {
			host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		}
		secure := tlsClient(conn, host)
		handshakeCtx, cancel := context.WithTimeout(ctx, timeout)
		err := secure.HandshakeContext(handshakeCtx)
		cancel()
		if err != nil {
			_ = conn.Close()
			return nil, &ConnectError{Err: err}
		}
		conn = secure
	}
	// The deadline covers the CONNECT exchange; the tunnel itself must be free.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}

	var head strings.Builder
	head.WriteString("CONNECT " + destAddr + " HTTP/1.1\r\nHost: " + destAddr + "\r\n")
	user, err := target.DecodedUser()
	if err != nil {
		_ = conn.Close()
		return nil, &ConnectError{Err: err}
	}
	pass, err := target.DecodedPass()
	if err != nil {
		_ = conn.Close()
		return nil, &ConnectError{Err: err}
	}
	if user != "" {
		credentials := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		head.WriteString("Proxy-Authorization: Basic " + credentials + "\r\n")
	}
	head.WriteString("\r\n")
	if _, err := conn.Write([]byte(head.String())); err != nil {
		_ = conn.Close()
		return nil, &ConnectError{Err: err}
	}

	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, &ConnectError{Err: err}
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, &ConnectError{Err: fmt.Errorf("proxy CONNECT answered %s", response.Status)}
	}
	_ = conn.SetDeadline(time.Time{})
	// Bytes the proxy already sent after its reply belong to the tunnel.
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

// bufferedConn drains whatever the CONNECT exchange buffered before reading the
// socket, so no tunnel byte is lost to the bufio.Reader.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// retryable reports whether an attempt failed before the origin could see the
// request, which is the only case a mutation is replayed for.
func retryable(err error) bool {
	var connect *ConnectError
	if errors.As(err, &connect) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return true
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		var timeout interface{ Timeout() bool }
		return errors.As(err, &timeout) && timeout.Timeout()
	}
	return false
}

// directClient is the transport used when the pool is off or a direct fallback
// runs. It ignores proxy environment variables.
func directClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 nil,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		MaxIdleConnsPerHost:   DefaultPolicy().Connections,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}}
}

// tlsClient wraps a tunnel in TLS, with normal certificate verification against
// host for both names and IP literals.
func tlsClient(conn net.Conn, host string) *tls.Conn {
	return tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
}
