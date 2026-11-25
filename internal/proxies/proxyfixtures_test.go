package proxies

// Loopback proxies for the pool tests: a real SOCKS5 server (with and without
// authentication), an HTTP CONNECT tunnel and a proxy that rejects CONNECT.

import (
	"bufio"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// --- SOCKS5 fixture -----------------------------------------------------

// socksServer is a real SOCKS5 server: it performs the handshake and forwards
// TCP, like the JS fixture in test/api/proxies.test.js.
type socksServer struct {
	listener net.Listener
	accepted atomic.Int32
}

func startSOCKS5(t *testing.T) *socksServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen socks: %v", err)
	}
	server := &socksServer{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			server.accepted.Add(1)
			go server.handle(conn)
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *socksServer) port() int { return s.listener.Addr().(*net.TCPAddr).Port }

// startSOCKS5Auth is a SOCKS5 server that requires username/password
// authentication (RFC 1929), so the client's credential path is exercised.
func startSOCKS5Auth(t *testing.T, user, pass string) *socksServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen socks: %v", err)
	}
	server := &socksServer{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			server.accepted.Add(1)
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				reader := bufio.NewReader(conn)
				greeting := make([]byte, 2)
				if _, err := io.ReadFull(reader, greeting); err != nil {
					return
				}
				methods := make([]byte, int(greeting[1]))
				if _, err := io.ReadFull(reader, methods); err != nil {
					return
				}
				offersPassword := false
				for _, method := range methods {
					if method == 2 {
						offersPassword = true
					}
				}
				if !offersPassword {
					_, _ = conn.Write([]byte{5, 0xff})
					return
				}
				if _, err := conn.Write([]byte{5, 2}); err != nil {
					return
				}
				head := make([]byte, 2)
				if _, err := io.ReadFull(reader, head); err != nil {
					return
				}
				givenUser := make([]byte, int(head[1]))
				if _, err := io.ReadFull(reader, givenUser); err != nil {
					return
				}
				length := make([]byte, 1)
				if _, err := io.ReadFull(reader, length); err != nil {
					return
				}
				givenPass := make([]byte, int(length[0]))
				if _, err := io.ReadFull(reader, givenPass); err != nil {
					return
				}
				if string(givenUser) != user || string(givenPass) != pass {
					_, _ = conn.Write([]byte{1, 1})
					return
				}
				if _, err := conn.Write([]byte{1, 0}); err != nil {
					return
				}
				server.serveConnect(conn, reader)
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *socksServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)

	greeting := make([]byte, 2)
	if _, err := io.ReadFull(reader, greeting); err != nil {
		return
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil { // no authentication
		return
	}
	s.serveConnect(conn, reader)
}

// serveConnect answers one CONNECT request and splices the tunnel.
func (s *socksServer) serveConnect(conn net.Conn, reader *bufio.Reader) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(reader, head); err != nil {
		return
	}
	if head[0] != 5 || head[1] != 1 { // version 5, CONNECT only
		return
	}
	var host string
	switch head[3] {
	case 1:
		address := make([]byte, 4)
		if _, err := io.ReadFull(reader, address); err != nil {
			return
		}
		host = net.IP(address).String()
	case 3:
		length := make([]byte, 1)
		if _, err := io.ReadFull(reader, length); err != nil {
			return
		}
		address := make([]byte, int(length[0]))
		if _, err := io.ReadFull(reader, address); err != nil {
			return
		}
		host = string(address)
	case 4:
		address := make([]byte, 16)
		if _, err := io.ReadFull(reader, address); err != nil {
			return
		}
		host = net.IP(address).String()
	default:
		return
	}
	rawPort := make([]byte, 2)
	if _, err := io.ReadFull(reader, rawPort); err != nil {
		return
	}
	port := int(rawPort[0])<<8 | int(rawPort[1])

	target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}) // connection refused
		return
	}
	defer func() { _ = target.Close() }()
	if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}
	go func() {
		_, _ = io.Copy(target, reader)
		_ = target.(*net.TCPConn).CloseWrite()
	}()
	_, _ = io.Copy(conn, target)
}

// --- HTTP CONNECT fixture -----------------------------------------------

// connectProxy is an HTTP proxy that only tunnels, recording the credentials it
// was given so a test can assert them.
type connectProxy struct {
	listener   net.Listener
	mu         sync.Mutex
	authHeader []string
}

func startConnectProxy(t *testing.T, requireAuth bool) *connectProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen connect proxy: %v", err)
	}
	server := &connectProxy{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.handle(conn, requireAuth)
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (p *connectProxy) port() int { return p.listener.Addr().(*net.TCPAddr).Port }

func (p *connectProxy) lastAuth() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.authHeader) == 0 {
		return ""
	}
	return p.authHeader[len(p.authHeader)-1]
}

func (p *connectProxy) handle(conn net.Conn, requireAuth bool) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.authHeader = append(p.authHeader, req.Header.Get("Proxy-Authorization"))
	p.mu.Unlock()
	if requireAuth {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:secret"))
		if req.Header.Get("Proxy-Authorization") != want {
			_, _ = io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
			return
		}
	}
	target, err := net.Dial("tcp", req.Host)
	if err != nil {
		_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer func() { _ = target.Close() }()
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	go func() { _, _ = io.Copy(target, reader) }()
	_, _ = io.Copy(conn, target)
}

// startDeniedProxy answers every CONNECT with 407.
func startDeniedProxy(t *testing.T) *connectProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen denied proxy: %v", err)
	}
	server := &connectProxy{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				reader := bufio.NewReader(conn)
				if _, err := http.ReadRequest(reader); err != nil {
					return
				}
				_, _ = io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

// closedPort returns a loopback port nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return port
}
