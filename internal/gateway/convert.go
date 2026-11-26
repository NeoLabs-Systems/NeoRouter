package gateway

// Conversions between the gateway's values and the other services' own types.
// Each of those packages declares its own narrow view of an entry, a key or a
// request; these functions are the boundary between them.

import (
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// float64Ptr is a pointer to a value.
func float64Ptr(value float64) *float64 { return &value }

// intPtr is a pointer to a value.
func intPtr(value int) *int { return &value }

// budgetKeyOf is the budget service's view of a router key.
func budgetKeyOf(key *Key) budget.Key {
	row := key.Row
	out := budget.Key{ID: row.ID, BudgetUSD: row.BudgetUSD, BudgetPeriod: row.BudgetPeriod, TokenPeriod: row.TokenPeriod}
	out.TokenLimit = row.TokenLimit
	return out
}

// priceOf is the budget service's view of a model's prices.
func priceOf(entry *routing.Entry) budget.Price {
	return budget.Price{
		IsFree:          entry.IsFree,
		IsPlan:          entry.IsPlan,
		PriceIn:         entry.PriceIn,
		PriceOut:        entry.PriceOut,
		PriceCachedIn:   entry.PriceCachedIn,
		PriceCacheWrite: entry.PriceCacheWrite,
	}
}

// budgetUsageOf is a normalized usage block as the budget service reads it.
func budgetUsageOf(tokens *Obj) *budget.Usage {
	if tokens == nil {
		return nil
	}
	prompt, _ := getNumber(tokens, "prompt_tokens")
	completion, _ := getNumber(tokens, "completion_tokens")
	cached, _ := getNumber(getObj(tokens, "prompt_tokens_details"), "cached_tokens")
	creation, _ := getNumber(tokens, "cache_creation_input_tokens")
	return &budget.Usage{
		PromptTokens:             int64(prompt),
		CompletionTokens:         int64(completion),
		CachedTokens:             int64(cached),
		CacheCreationInputTokens: int64(creation),
	}
}

// quotaEntryOf is the quota service's view of a model.
func quotaEntryOf(entry *routing.Entry) quota.Entry {
	return quota.Entry{
		Ref:            entry.Ref,
		ID:             entry.ID,
		Name:           entry.Name,
		AccountID:      entry.AccountID,
		DefinitionID:   entry.DefinitionID,
		ModelID:        entry.ModelID,
		IsFree:         entry.IsFree,
		IsPlan:         entry.IsPlan,
		IsFlatRate:     entry.IsFlatRate,
		Enabled:        entry.Enabled,
		AccountEnabled: entry.AccountEnabled,
		Available:      entry.Available,
	}
}

// requestInfoOf is the routing view of a canonical chat request: the messages,
// the tools and the response format the classifier reads.
func requestInfoOf(canonical *Obj) routing.RequestInfo {
	info := routing.RequestInfo{}
	for _, rawMessage := range getList(canonical, "messages") {
		message := asObj(rawMessage)
		converted := task.Message{
			Role:       getString(message, "role"),
			Name:       getString(message, "name"),
			ToolCallID: getString(message, "tool_call_id"),
		}
		converted.Content = taskContentOf(getValue(message, "content"))
		for _, rawCall := range getList(message, "tool_calls") {
			call := asObj(rawCall)
			function := getObj(call, "function")
			converted.ToolCalls = append(converted.ToolCalls, task.ToolCall{
				ID:   getString(call, "id"),
				Type: getString(call, "type"),
				Name: getString(call, "name"),
				Function: task.Function{
					Name:      getString(function, "name"),
					Arguments: getString(function, "arguments"),
				},
			})
		}
		info.Messages = append(info.Messages, converted)
	}
	for _, rawTool := range getList(canonical, "tools") {
		tool := asObj(rawTool)
		function := getObj(tool, "function")
		info.Tools = append(info.Tools, task.Tool{
			Type: getString(tool, "type"),
			Function: task.ToolFunction{
				Name:        getString(function, "name"),
				Description: getString(function, "description"),
				Parameters:  getValue(function, "parameters"),
			},
		})
	}
	if format := getObj(canonical, "response_format"); format != nil {
		info.ResponseFormat = &task.ResponseFormat{Type: getString(format, "type")}
	}
	if maxTokens, ok := getNumber(canonical, "max_tokens"); ok {
		info.MaxTokens = intPtr(int(maxTokens))
	}
	return info
}

// taskContentOf is the classifier's view of a message's content: the text, or
// the parts.
func taskContentOf(content any) any {
	if text, ok := content.(string); ok {
		return text
	}
	parts := ContentParts(content)
	if len(parts) == 0 {
		return nil
	}
	out := make([]task.Part, 0, len(parts))
	for _, rawPart := range parts {
		part := asObj(rawPart)
		out = append(out, task.Part{Type: getString(part, "type"), Text: getString(part, "text")})
	}
	return out
}

// routingFailureOf is a classified provider failure as routing learns from it.
func routingFailureOf(err *providers.ProviderError) routing.Failure {
	if err == nil {
		return routing.Failure{Kind: routing.KindServer, Message: "The provider failed."}
	}
	failure := routing.Failure{
		Kind:        routing.Kind(err.Kind),
		Message:     err.Message,
		Status:      err.Status,
		NeedsAction: err.NeedsAction,
		Headers:     err.Headers,
	}
	if err.RetryAfterKnown {
		retryAfter := err.RetryAfter
		failure.RetryAfter = &retryAfter
	}
	if err.Learned != nil {
		failure.Learned = &routing.LearnedLimit{Metric: err.Learned.Metric, Value: err.Learned.Value}
	}
	return failure
}

// classify is fromNetworkError: a failure from an adapter or the transport, as
// the classified provider error routing reacts to.
func classify(err error) *providers.ProviderError {
	if err == nil {
		return nil
	}
	return providers.FromNetwork(err)
}

// measurementOf is what one attempt measured, as routing records it.
func measurementOf(latencyMs float64, ttftMs *float64, outputTokens int, generationMs float64) routing.Measurement {
	return routing.Measurement{
		LatencyMs:    float64Ptr(latencyMs),
		TTFTMs:       ttftMs,
		OutputTokens: float64(outputTokens),
		GenerationMs: generationMs,
	}
}

// millis is a duration in milliseconds.
func millis(duration time.Duration) float64 { return float64(duration) / float64(time.Millisecond) }
