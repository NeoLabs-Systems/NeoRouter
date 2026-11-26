package gateway

// Stream plumbing shared by the translators: canonical chunk streams and the
// SSE events a client dialect receives.

import (
	"iter"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
)

// Event is one server-sent event: an optional event name and a data payload
// (an *Obj, a string such as "[DONE]", or anything JSON-encodable).
type Event struct {
	Event string
	Data  any
}

// ChunkStream is a canonical chat chunk stream: the chunks as they arrive, then
// either a nil error (the stream ended) or the failure that ended it. It is the
// Go shape of the async iterator adapter.stream() returns.
type ChunkStream = iter.Seq2[*Obj, error]

// clone is a shallow copy of an ordered object.
func clone(object *Obj) *Obj {
	if object == nil {
		return nil
	}
	return object.Clone()
}

// setAll copies every key of from into to, keeping to's order.
func setAll(to, from *Obj) {
	if to == nil || from == nil {
		return
	}
	for _, key := range from.Keys() {
		value, _ := from.Get(key)
		to.Set(key, value)
	}
}

// newObjOf builds an ordered object from alternating keys and values.
func newObjOf(pairs ...any) *Obj {
	out := transport.NewObj()
	for i := 0; i+1 < len(pairs); i += 2 {
		key, _ := pairs[i].(string)
		out.Set(key, pairs[i+1])
	}
	return out
}
