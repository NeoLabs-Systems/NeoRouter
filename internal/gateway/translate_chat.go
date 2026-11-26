package gateway

// OpenAI Chat Completions is the canonical format, so this translator only
// normalizes the request and relabels the model the client asked for.

import "iter"

// ChatTranslator is OpenAI Chat Completions, the canonical dialect.
type ChatTranslator struct{}

// ToCanonical normalizes the client body.
func (ChatTranslator) ToCanonical(body *Obj) (*Obj, error) { return NormalizeRequest(body), nil }

// ResponseFromCanonical relabels the model the client asked for.
func (ChatTranslator) ResponseFromCanonical(completion *Obj, model string) *Obj {
	out := clone(completion)
	if out == nil {
		out = newObj()
	}
	out.Set("model", model)
	return out
}

// StreamFromCanonical relays canonical chunks, then [DONE].
func (ChatTranslator) StreamFromCanonical(chunks iter.Seq2[*Obj, error], model string, yield func(Event) bool) error {
	for chunk, err := range chunks {
		if err != nil {
			return err
		}
		out := clone(chunk)
		out.Set("model", model)
		if !yield(Event{Data: out}) {
			return nil
		}
	}
	yield(Event{Data: "[DONE]"})
	return nil
}
