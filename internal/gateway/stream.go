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

// StreamFromChunks is a ChunkStream over a fixed list of chunks.
func StreamFromChunks(chunks ...*Obj) ChunkStream {
	return func(yield func(*Obj, error) bool) {
		for _, chunk := range chunks {
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

// FailingStream is a ChunkStream of the chunks, then the error.
func FailingStream(err error, chunks ...*Obj) ChunkStream {
	return func(yield func(*Obj, error) bool) {
		for _, chunk := range chunks {
			if !yield(chunk, nil) {
				return
			}
		}
		yield(nil, err)
	}
}

// CollectChunks drains a ChunkStream into a list, stopping at the first error.
func CollectChunks(stream ChunkStream) ([]*Obj, error) {
	var out []*Obj
	for chunk, err := range stream {
		if err != nil {
			return out, err
		}
		out = append(out, chunk)
	}
	return out, nil
}

// collectEvents drains a translator's event stream, for tests and for callers
// that answer without a live connection.
func collectEvents(stream func(yield func(Event) bool) error) ([]Event, error) {
	var out []Event
	err := stream(func(event Event) bool {
		out = append(out, event)
		return true
	})
	return out, err
}

// clone is a shallow copy of an ordered object (JavaScript's {...object}).
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
