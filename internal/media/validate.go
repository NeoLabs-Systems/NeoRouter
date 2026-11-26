package media

import (
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// The request bodies of the media endpoints are validated property by property:
// the first failing property in schema order decides, and its message is
// "<path>: <message>", which is the error text the client sees.

const (
	maxEmbeddingInputs = 2048
	maxAudioBytes      = 25 * 1024 * 1024
)

// issue is one validation failure: where it is, and the message. dirty marks a
// check failure (a length, a range, an enum member) rather than a type failure,
// because a union prefers a branch whose type fits the value even when that
// branch then fails a check.
type issue struct {
	path    string
	message string
	dirty   bool
}

// err renders the issue as the 400 the API returns.
func (i *issue) err() error {
	path := i.path
	if path == "" {
		path = "body"
	}
	return httpserver.New(http.StatusBadRequest, "INVALID_REQUEST", path+": "+i.message)
}

// objectBody reads a decoded JSON request body as the object the schemas
// expect: a falsy body (absent, null, "", 0, false) is an empty object, and any
// other shape is "Expected object, received ...".
func objectBody(body any) (map[string]any, *issue) {
	switch value := body.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return value, nil
	case string:
		if value == "" {
			return map[string]any{}, nil
		}
	case float64:
		if value == 0 {
			return map[string]any{}, nil
		}
	case bool:
		if !value {
			return map[string]any{}, nil
		}
	}
	return nil, typeIssue("body", "object", body)
}

// fields reads the members of one request body, remembering the first issue it
// found so a schema function can read in declaration order.
type fields struct {
	members map[string]any
	issue   *issue
}

func newFields(body any) *fields {
	members, found := objectBody(body)
	return &fields{members: members, issue: found}
}

// fail records an issue; the first one wins.
func (f *fields) fail(found *issue) {
	if f.issue == nil && found != nil {
		f.issue = found
	}
}

// err is the request's error, nil when every property passed.
func (f *fields) err() error {
	if f.issue == nil {
		return nil
	}
	return f.issue.err()
}

// optional reads one member. Only an absent member is "not sent": null is a
// value of its own type and is refused like any other value of the wrong type.
func (f *fields) optional(name string) (any, bool) {
	value, present := f.members[name]
	return value, present
}
