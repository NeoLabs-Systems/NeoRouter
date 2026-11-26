package httpserver

import "net/http"

// MethodFallback answers a wrong-method request the way Express did: the JSON
// 404 envelope, not ServeMux's empty 405 with an Allow header. No NeoRouter
// route answers 405 deliberately, so every 405 the mux produces is a fallthrough.
func MethodFallback(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&methodRewriter{ResponseWriter: w}, r)
	})
}

type methodRewriter struct {
	http.ResponseWriter
	rewrote bool
}

func (m *methodRewriter) WriteHeader(status int) {
	if status == http.StatusMethodNotAllowed {
		m.rewrote = true
		m.ResponseWriter.Header().Del("Allow")
		NotFound(m.ResponseWriter)
		return
	}
	m.ResponseWriter.WriteHeader(status)
}

func (m *methodRewriter) Write(body []byte) (int, error) {
	if m.rewrote {
		return len(body), nil
	}
	return m.ResponseWriter.Write(body)
}

// Unwrap keeps http.ResponseController (flush, hijack for WebSockets) working
// through the wrapper.
func (m *methodRewriter) Unwrap() http.ResponseWriter { return m.ResponseWriter }
