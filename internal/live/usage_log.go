package live

import (
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// recordUsage writes the request log row for a finished session
// (usage.record in the gateway). It never breaks a session: a log that cannot be
// written is reported and the session has already ended anyway.
func (s *session) recordUsage(entry *routing.Entry, reason, errorMessage, requestedModel string,
	trail []any, tokens liveTokens, cost float64, seconds int64, now time.Time) {
	if s.svc.opts.Usage == nil {
		return
	}
	trailJSON, err := marshalJSON(trail)
	if err != nil {
		trailJSON = []byte("[]")
	}
	keyID := s.key.ID
	attempts := int64(len(trail))
	if attempts < 1 {
		attempts = 1
	}
	latency := now.Sub(s.startedAt).Milliseconds()
	row := usage.Entry{
		ID:             s.id,
		UserID:         s.key.UserID,
		RouterKeyID:    &keyID,
		CreatedAt:      s.startedAt.UnixMilli(),
		APIFormat:      s.dialect.id,
		RequestedModel: requestedModel,
		Attempts:       attempts,
		Stream:         true,
		TrailJSON:      string(trailJSON),
		LatencyMS:      &latency,
		InputTokens:    int64(tokens.input),
		OutputTokens:   int64(tokens.output),
		Capability:     "live",
		Units:          float64(seconds),
	}
	if requestedModel == "" {
		row.RequestedModel = "auto"
	}
	if entry != nil {
		modelRef := entry.Ref
		modelLabel := entry.ID
		accountID := entry.AccountID
		row.Status = "success"
		row.HTTPStatus = int64PtrOf(101)
		row.ModelRef = &modelRef
		row.ModelLabel = &modelLabel
		row.AccountID = &accountID
		row.CostUSD = cost
		row.IsFree = entry.IsFree
	} else {
		// A session that never opened has no model to blame: 502 as a gateway that
		// could not reach one.
		errorCode := reason
		row.Status = "error"
		row.HTTPStatus = int64PtrOf(502)
		row.ErrorCode = &errorCode
		if errorMessage != "" {
			message := errorMessage
			row.ErrorMessage = &message
		}
		row.CostUSD = 0
		row.IsFree = false
	}
	if err := s.svc.opts.Usage.Record(row); err != nil {
		s.svc.log().Warn("Live session: record usage", "sessionId", s.id, "error", err)
	}
}

func int64PtrOf(value int64) *int64 { return &value }

// marshalJSON encodes a value the way JSON.stringify does: no HTML escaping and
// no trailing newline.
func marshalJSON(value any) ([]byte, error) {
	var buf strings.Builder
	encoder := jsonEncoder(&buf)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(buf.String(), "\n")), nil
}
