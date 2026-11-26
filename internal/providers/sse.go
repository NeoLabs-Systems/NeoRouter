package providers

import (
	"bufio"
	"io"
	"iter"
	"strings"
)

// Server-Sent Events: the chat stream format every provider speaks a dialect
// of. The reader keeps standard event boundaries and comments.

// SSEEvent is one Server-Sent Event: the event name (empty when the stream
// names none) and the raw data it carried.
type SSEEvent struct {
	Event string
	Data  string
}

// SSE reads a Server-Sent Events stream. Each event's data is the raw string;
// the caller decides how to parse it. An error ends the iteration.
func SSE(body io.Reader) iter.Seq2[SSEEvent, error] {
	return func(yield func(SSEEvent, error) bool) {
		reader := bufio.NewReader(body)
		var event *string
		var dataLines []string
		for {
			line, err := readSSELine(reader)
			if err != nil {
				if err != io.EOF {
					yield(SSEEvent{}, FromNetwork(err))
					return
				}
				break
			}
			if line == "" {
				if len(dataLines) > 0 {
					if !yield(sseEvent(event, dataLines), nil) {
						return
					}
				}
				event = nil
				dataLines = nil
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			field, value, found := strings.Cut(line, ":")
			if found {
				value = strings.TrimPrefix(value, " ")
			}
			switch field {
			case "event":
				named := value
				event = &named
			case "data":
				dataLines = append(dataLines, value)
			}
		}
		if len(dataLines) > 0 {
			yield(sseEvent(event, dataLines), nil)
		}
	}
}

// sseEvent joins one event's data lines.
func sseEvent(event *string, dataLines []string) SSEEvent {
	dispatched := SSEEvent{Data: strings.Join(dataLines, "\n")}
	if event != nil {
		dispatched.Event = *event
	}
	return dispatched
}

// readSSELine returns one line without its terminator. A partial line at the
// end of the stream is dropped.
func readSSELine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
