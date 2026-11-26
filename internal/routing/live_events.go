package routing

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// What the router is doing right now, per user, for the home screen's live view
// (live_events.js): a request started, went to a provider, a provider failed
// it, it finished. Kept in memory only (a short ring of events and the requests
// still in flight) and read by long polling. Never holds prompt text.
const (
	liveRing  = 300
	liveStale = 15 * minuteMs
)

// LiveEvent is one step of the live view.
type LiveEvent struct {
	Seq     int64
	At      int64
	Type    string // start | attempt | fail | done
	ID      string
	Key     string
	Task    string
	Slug    string
	Model   string
	Success *bool
}

// MarshalJSON keeps the Node code's key order per event type.
func (e LiveEvent) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteString(`{"seq":`)
	writeJSON(&b, e.Seq)
	b.WriteString(`,"at":`)
	writeJSON(&b, e.At)
	b.WriteString(`,"type":`)
	writeJSON(&b, e.Type)
	b.WriteString(`,"id":`)
	writeJSON(&b, e.ID)
	switch e.Type {
	case "start":
		b.WriteString(`,"key":`)
		writeJSON(&b, e.Key)
		// A request without a task kind leaves the key out, as an undefined
		// property does in the Node code.
		if e.Task != "" {
			b.WriteString(`,"task":`)
			writeJSON(&b, e.Task)
		}
	case "attempt":
		b.WriteString(`,"slug":`)
		writeJSON(&b, e.Slug)
		b.WriteString(`,"model":`)
		writeJSON(&b, e.Model)
	case "fail":
		b.WriteString(`,"slug":`)
		writeJSON(&b, e.Slug)
	case "done":
		b.WriteString(`,"success":`)
		writeJSON(&b, e.Success != nil && *e.Success)
		b.WriteString(`,"model":`)
		writeJSON(&b, e.Model)
	}
	b.WriteString("}")
	return []byte(b.String()), nil
}

func writeJSON(b *strings.Builder, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		b.WriteString("null")
		return
	}
	b.Write(encoded)
}

// LiveAttempt is one provider attempt of a live request.
type LiveAttempt struct {
	Slug  string `json:"slug"`
	Model string `json:"model"`
	State string `json:"state"`
}

// LiveRequest is a request still in flight.
type LiveRequest struct {
	ID        string        `json:"id"`
	Key       string        `json:"key"`
	Task      *string       `json:"task,omitempty"`
	StartedAt int64         `json:"startedAt"`
	Attempts  []LiveAttempt `json:"attempts"`
}

// LiveRead is what GET /routing/live answers.
type LiveRead struct {
	Seq      int64         `json:"seq"`
	Snapshot bool          `json:"snapshot"`
	InFlight []LiveRequest `json:"inFlight,omitempty"`
	Events   []LiveEvent   `json:"events"`
}

type liveUser struct {
	seq      int64
	events   []LiveEvent
	inFlight map[string]*LiveRequest
	changed  chan struct{}
}

type liveFeed struct {
	mu    sync.Mutex
	users map[string]*liveUser
}

func newLiveFeed() *liveFeed {
	return &liveFeed{users: map[string]*liveUser{}}
}

func (f *liveFeed) state(userID string) *liveUser {
	state, ok := f.users[userID]
	if !ok {
		state = &liveUser{inFlight: map[string]*LiveRequest{}, changed: make(chan struct{})}
		f.users[userID] = state
	}
	return state
}

// push appends an event and wakes long polls.
func (f *liveFeed) push(userID string, event LiveEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.state(userID)
	state.seq++
	event.Seq = state.seq
	event.At = time.Now().UnixMilli()
	state.events = append(state.events, event)
	if len(state.events) > liveRing {
		state.events = state.events[len(state.events)-liveRing:]
	}
	close(state.changed)
	state.changed = make(chan struct{})
}

// LiveStarted notes a request that started; key is the card it comes from (a
// key's name, or the pool).
func (s *Service) LiveStarted(userID, id, key, task string) {
	request := &LiveRequest{ID: id, Key: key, StartedAt: time.Now().UnixMilli(), Attempts: []LiveAttempt{}}
	if task != "" {
		request.Task = &task
	}
	s.live.mu.Lock()
	s.live.state(userID).inFlight[id] = request
	s.live.mu.Unlock()
	s.live.push(userID, LiveEvent{Type: "start", ID: id, Key: key, Task: task})
}

// LiveAttempt notes that an attempt went to a connection.
func (s *Service) LiveAttempt(userID, id, slug, model string) {
	s.live.mu.Lock()
	if state, ok := s.live.users[userID]; ok {
		if request, ok := state.inFlight[id]; ok {
			request.Attempts = append(request.Attempts, LiveAttempt{Slug: slug, Model: model, State: "running"})
		}
	}
	s.live.mu.Unlock()
	s.live.push(userID, LiveEvent{Type: "attempt", ID: id, Slug: slug, Model: model})
}

// LiveFailed notes that a provider failed an attempt.
func (s *Service) LiveFailed(userID, id, slug string) {
	s.live.mu.Lock()
	if state, ok := s.live.users[userID]; ok {
		if request, ok := state.inFlight[id]; ok {
			for i := range request.Attempts {
				attempt := &request.Attempts[i]
				if attempt.Slug == slug && attempt.State == "running" {
					attempt.State = "failed"
					break
				}
			}
		}
	}
	s.live.mu.Unlock()
	s.live.push(userID, LiveEvent{Type: "fail", ID: id, Slug: slug})
}

// LiveFinished notes that a request ended.
func (s *Service) LiveFinished(userID, id string, success bool, model string) {
	s.live.mu.Lock()
	state, ok := s.live.users[userID]
	if ok {
		delete(state.inFlight, id)
	}
	s.live.mu.Unlock()
	// A request that ended without saying so does not push.
	if !ok {
		return
	}
	s.live.push(userID, LiveEvent{Type: "done", ID: id, Success: &success, Model: model})
}

// ReadLive returns the events after since. When there are none yet, it waits up
// to wait for the next one. A since that is no longer in the ring (or 0) gets a
// snapshot of what is in flight instead, so the view starts mid-traffic.
func (s *Service) ReadLive(ctx context.Context, userID string, since int64, wait time.Duration) LiveRead {
	s.live.mu.Lock()
	state := s.live.state(userID)
	now := time.Now().UnixMilli()
	// A request that ended without saying so (an error before it was recorded)
	// does not stay.
	for id, request := range state.inFlight {
		if now-request.StartedAt > liveStale {
			delete(state.inFlight, id)
		}
	}
	oldest := state.seq + 1
	if len(state.events) > 0 {
		oldest = state.events[0].Seq
	}
	if since == 0 || since < oldest-1 || since > state.seq {
		result := LiveRead{Seq: state.seq, Snapshot: true, InFlight: []LiveRequest{}, Events: []LiveEvent{}}
		for _, request := range state.inFlight {
			result.InFlight = append(result.InFlight, *request)
		}
		s.live.mu.Unlock()
		return result
	}
	if since == state.seq && wait > 0 {
		changed := state.changed
		s.live.mu.Unlock()
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-changed:
		case <-timer.C:
		case <-ctx.Done():
		}
		s.live.mu.Lock()
		state = s.live.state(userID)
	}
	result := LiveRead{Seq: state.seq, Events: []LiveEvent{}}
	for _, event := range state.events {
		if event.Seq > since {
			result.Events = append(result.Events, event)
		}
	}
	s.live.mu.Unlock()
	return result
}
