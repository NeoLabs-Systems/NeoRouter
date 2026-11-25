package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// clientSessionHeaders are the headers a client may name its conversation in.
// Some hosts (OpenCode) route and cache by a conversation id the client sends,
// so the client's own id is passed through.
var clientSessionHeaders = []string{
	"x-opencode-session",
	"x-session-id",
	"x-session-affinity",
	"x-claude-code-session-id",
	"session_id",
	"session-id",
}

// safeSession is the shape a client-supplied session id must have to be passed
// on: nothing that would break a header or mean something to a provider.
var safeSession = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// ClientSession returns the conversation id the request carries, or "" when it
// carries none (or carries something unusable).
func ClientSession(r *http.Request) string {
	if r == nil {
		return ""
	}
	for _, name := range clientSessionHeaders {
		value := strings.TrimSpace(r.Header.Get(name))
		if safeSession.MatchString(value) {
			return value
		}
	}
	return ""
}

// DerivedSession is the conversation id for a request that carries none: it is
// derived from the start of the conversation, so every later turn of the same
// conversation gets the same id and a different conversation gets another. The
// canonical request is the OpenAI wire body the gateway forwards.
func DerivedSession(canonical json.RawMessage, salt string) string {
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(canonical, &request)
	head := []string{"", ""}
	var foundSystem, foundUser bool
	for _, message := range request.Messages {
		switch {
		case !foundSystem && (message.Role == "system" || message.Role == "developer"):
			head[0], foundSystem = contentText(message.Content), true
		case !foundUser && message.Role == "user":
			head[1], foundUser = contentText(message.Content), true
		}
	}
	digest := sha256.Sum256([]byte(salt + "\n" + strings.Join(head, "\n")))
	return "nr-" + hex.EncodeToString(digest[:])[:32]
}

// contentText is the text of a message's content, which is a string or a list
// of parts.
func contentText(content json.RawMessage) string {
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			builder.WriteString(part.Text)
		}
		return builder.String()
	}
	return ""
}
