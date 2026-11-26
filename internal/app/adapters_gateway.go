package app

// The gateway's hooks, the media capability's pipeline and key auth, the
// processing bridge the gateway shields requests with, and the pool seams both
// sides of the share policy read.

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/gateway"
	"github.com/NeoLabs-Systems/neorouter/internal/media"
	"github.com/NeoLabs-Systems/neorouter/internal/pools"
	"github.com/NeoLabs-Systems/neorouter/internal/processing"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/teams"
)

// gatewayAccounts is the provider account service as the gateway reads it.
type gatewayAccounts struct {
	accounts *providers.Service
	registry *providers.Registry
}

func (g gatewayAccounts) Adapter(accountID string) providers.Adapter {
	ctx := context.Background()
	runtime, err := g.accounts.Runtime(ctx, accountID)
	if err != nil || runtime == nil {
		return nil
	}
	adapter, err := g.accounts.AdapterFor(ctx, runtime)
	if err != nil {
		return nil
	}
	return adapter
}

func (g gatewayAccounts) CanServe(accountID string, method providers.Capability) bool {
	can, err := g.accounts.CanServe(context.Background(), accountID, method)
	return err == nil && can
}

// Timeout is the account's upstream timeout (adapter.timeoutMs): the
// definition's own override, else 0 so the configured timeout applies.
func (g gatewayAccounts) Timeout(accountID string) time.Duration {
	runtime, err := g.accounts.Runtime(context.Background(), accountID)
	if err != nil || runtime == nil {
		return 0
	}
	def, ok := g.registry.Get(runtime.DefinitionID)
	if !ok {
		return 0
	}
	return time.Duration(def.Quirks.TimeoutMS) * time.Millisecond
}

func (g gatewayAccounts) Validate(accountID string) {
	_, _ = g.accounts.ValidateAccount(context.Background(), accountID)
}

func (g gatewayAccounts) LiveProtocols(accountID string) []string {
	runtime, err := g.accounts.Runtime(context.Background(), accountID)
	if err != nil || runtime == nil {
		return nil
	}
	def, ok := g.registry.Get(runtime.DefinitionID)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(def.Live))
	for _, dialect := range def.Live {
		out = append(out, string(dialect))
	}
	return out
}

// mediaRunner runs one non-chat capability through the gateway's shared
// pipeline (media.RunOptions maps onto gateway.CapabilityRun one to one).
type mediaRunner struct {
	gateway *gateway.Service
}

func (r mediaRunner) Run(ctx context.Context, options *media.RunOptions) (*media.Result, error) {
	var key *gateway.Key
	if options.Key != nil {
		key = gateway.KeyOf(options.Key)
	}
	contextTokens := options.InputTokens
	if options.ContextTokens != nil {
		contextTokens = *options.ContextTokens
	}
	run := &gateway.CapabilityRun{
		W:              options.Response,
		R:              options.Request,
		Key:            key,
		Capability:     options.Capability,
		Method:         options.Method,
		RequestedModel: options.RequestedModel,
		InputTokens:    options.InputTokens,
		ContextTokens:  contextTokens,
		Units:          options.Units,
		Format:         options.Format,
		FreeOnly:       options.FreeOnly,
		RedactedCount:  int64(options.RedactedCount),
	}
	if options.Filter != nil {
		filter := options.Filter
		run.Filter = func(entry *routing.Entry, adapter providers.Adapter) bool {
			return filter(gateway.ToCatalogEntry(entry), adapter)
		}
	}
	if options.Invoke != nil {
		invoke := options.Invoke
		run.Invoke = func(ctx context.Context, entry *routing.Entry, adapter providers.Adapter, timeout time.Duration) (*gateway.CapabilityResult, error) {
			outcome, err := invoke(ctx, media.Attempt{
				Entry:   gateway.ToCatalogEntry(entry),
				Adapter: adapter,
				Timeout: timeout,
			})
			if err != nil {
				return nil, err
			}
			if outcome == nil {
				return &gateway.CapabilityResult{}, nil
			}
			return &gateway.CapabilityResult{
				Value:       outcome.Value,
				Headers:     outcome.Headers,
				Units:       outcome.Units,
				InputTokens: outcome.InputTokens,
			}, nil
		}
	}
	if options.CostOf != nil {
		costOf := options.CostOf
		run.CostOf = func(entry *routing.Entry, result *gateway.CapabilityResult) float64 {
			cost := costOf(gateway.ToCatalogEntry(entry), outcomeOf(result))
			if cost == nil {
				return 0
			}
			return *cost
		}
	}
	if options.EstimateCost != nil {
		estimate := options.EstimateCost
		run.EstimateCost = func(entry *routing.Entry) *float64 {
			return estimate(gateway.ToCatalogEntry(entry))
		}
	}
	run.OnNoCandidate = options.NoCandidate
	if options.Respond != nil {
		respond := options.Respond
		writer := options.Response
		run.Respond = func(result *gateway.CapabilityResult, entry *routing.Entry) error {
			return respond(writer, outcomeOf(result), gateway.ToCatalogEntry(entry))
		}
	}
	result, entry, err := r.gateway.RunCapability(run)
	if err != nil {
		return nil, err
	}
	out := &media.Result{Outcome: outcomeOf(result)}
	if entry != nil {
		out.Entry = gateway.ToCatalogEntry(entry)
	}
	return out, nil
}

// outcomeOf is the gateway's capability result as the media domain reports it.
func outcomeOf(result *gateway.CapabilityResult) *media.Outcome {
	if result == nil {
		return &media.Outcome{}
	}
	return &media.Outcome{
		Value:       result.Value,
		InputTokens: result.InputTokens,
		Units:       result.Units,
		Headers:     result.Headers,
	}
}

// mediaKeyAuth is the gateway's router-key middleware as the media routes read
// it.
type mediaKeyAuth struct {
	gateway *gateway.Service
}

func (a mediaKeyAuth) Require(next http.Handler) http.Handler { return a.gateway.Require(next) }

func (a mediaKeyAuth) Key(r *http.Request) (*routerkeys.Row, bool) {
	return a.gateway.KeyRowOfRequest(r)
}

// processingBridge connects the gateway's canonical request to the processing
// domain: the shield and the compression pass on the way out, the restore on the
// way back.
type processingBridge struct {
	svc *processing.Service
}

// Apply runs compression and the privacy shield over one canonical request.
func (b processingBridge) Apply(ctx context.Context, userID string, canonical *gateway.Obj, options gateway.ProcessingOptions) (gateway.ProcessingResult, error) {
	result, err := b.svc.Apply(ctx, userID, canonicalMap(canonical), processing.ApplyOptions{
		CompressionLevel: options.CompressionLevel,
		PoolMinimum:      options.PoolMinimum,
	})
	if err != nil {
		return gateway.ProcessingResult{}, err
	}
	out := gateway.ProcessingResult{
		Canonical:        canonicalWith(canonical, result.Canonical),
		Vault:            result.Vault,
		Redacted:         int64(result.Stats.Redacted),
		CompressionLevel: result.Stats.Compression.Level,
		SavedTokens:      int64(result.Stats.Compression.SavedTokens),
	}
	return out, nil
}

// Restore puts the shielded values back into a finished completion.
func (b processingBridge) Restore(completion *gateway.Obj, vault any) *gateway.Obj {
	typed, ok := vault.(*processing.Vault)
	if !ok || typed == nil || completion == nil {
		return completion
	}
	restored := processing.RestoreResponse(canonicalMap(completion), typed)
	return canonicalWith(completion, restored)
}

// StreamRestorer is the restorer of one streamed answer, nil without a vault.
func (b processingBridge) StreamRestorer(vault any) gateway.StreamRestorer {
	typed, ok := vault.(*processing.Vault)
	if !ok || typed == nil {
		return nil
	}
	return streamRestorer{inner: processing.NewStreamRestorer(typed)}
}

// streamRestorer is processing's stream restorer in the gateway's shape.
type streamRestorer struct {
	inner *processing.StreamRestorer
}

func (s streamRestorer) Process(chunk *gateway.Obj) *gateway.Obj {
	restored := s.inner.Process(canonicalMap(chunk))
	return canonicalWith(chunk, restored)
}

func (s streamRestorer) Flush(template *gateway.Obj) *gateway.Obj {
	flushed := s.inner.Flush(canonicalMap(template))
	if len(flushed) == 0 {
		return nil
	}
	return canonicalWith(template, flushed)
}

// canonicalMap is a gateway object as the processing domain's canonical map,
// nested objects included.
func canonicalMap(obj *gateway.Obj) map[string]any {
	out := make(map[string]any, len(obj.Keys()))
	for _, key := range obj.Keys() {
		value, _ := obj.Get(key)
		out[key] = canonicalValue(value)
	}
	return out
}

// canonicalValue is one value of a canonical object in the processing domain's
// plain Go shape.
func canonicalValue(value any) any {
	switch typed := value.(type) {
	case *gateway.Obj:
		return canonicalMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = canonicalValue(item)
		}
		return out
	default:
		return value
	}
}

// canonicalWith merges a processed canonical map back into the object it came
// from, so the key order the client sent survives.
func canonicalWith(original *gateway.Obj, processed map[string]any) *gateway.Obj {
	if original == nil {
		return nil
	}
	out := original.Clone()
	for _, key := range out.Keys() {
		if _, ok := processed[key]; !ok {
			out.Delete(key)
			continue
		}
	}
	mergeInto(out, processed)
	return out
}

// mergeInto sets every key of the map on the object, keeping the position of the
// keys it already had; nested objects are rebuilt from their map.
func mergeInto(out *gateway.Obj, values map[string]any) {
	for key, value := range values {
		if existing, ok := out.Get(key); ok {
			if object, isObject := existing.(*gateway.Obj); isObject {
				if nested, isMap := value.(map[string]any); isMap {
					out.Set(key, canonicalWith(object, nested))
					continue
				}
			}
			if list, isList := existing.([]any); isList {
				if next, isNextList := value.([]any); isNextList {
					out.Set(key, mergeList(list, next))
					continue
				}
			}
		}
		out.Set(key, value)
	}
}

// mergeList merges a processed list back into the list it came from, item by
// item, keeping object order inside each item.
func mergeList(original []any, processed []any) []any {
	out := make([]any, len(processed))
	for i, item := range processed {
		if i < len(original) {
			if object, isObject := original[i].(*gateway.Obj); isObject {
				if nested, isMap := item.(map[string]any); isMap {
					out[i] = canonicalWith(object, nested)
					continue
				}
			}
			if list, isList := original[i].([]any); isList {
				if next, isNextList := item.([]any); isNextList {
					out[i] = mergeList(list, next)
					continue
				}
			}
		}
		out[i] = item
	}
	return out
}

// poolHooks are the pool seams the gateway reads: who may use the pool, the
// admitted share of a partner request, and what a partner is offered.
type poolHooks struct {
	pools *pools.Service
	now   func() time.Time
}

func (h poolHooks) PoolAccess(key *gateway.Key) gateway.PoolPermission {
	if key == nil || key.Row == nil {
		return gateway.PoolPermission{}
	}
	ctx := context.Background()
	privacy := h.pools.PoolPrivacySettings(ctx, key.Row.UserID)
	return gateway.PoolPermission{
		MayUse:               h.pools.MayUsePool(ctx, key.Row),
		KeepSensitivePrivate: privacy.KeepSensitivePrivate,
	}
}

func (h poolHooks) ShareAdmit(key *gateway.Key) *gateway.Share {
	if key == nil || key.Row == nil {
		return nil
	}
	admission, err := h.pools.Admit(context.Background(), key.Row, h.now().UnixMilli())
	if err != nil || admission == nil {
		return nil
	}
	share := &gateway.Share{
		Headers: map[string]string{},
		PoolID:  admission.PoolID,
		Release: admission.Release,
	}
	for name, values := range admission.Headers {
		if len(values) > 0 {
			share.Headers[name] = values[0]
		}
	}
	if admission.Reserve != nil {
		reserve := float64(*admission.Reserve)
		share.Reserve = &reserve
	}
	return share
}

func (h poolHooks) SharingWith(key *gateway.Key) bool {
	if key == nil || key.Row == nil {
		return false
	}
	return h.pools.SharingWith(context.Background(), key.Row)
}

func (h poolHooks) Offers(key *gateway.Key, entry *routing.Entry) bool {
	if key == nil || key.Row == nil || entry == nil {
		return false
	}
	ctx := context.Background()
	poolID := h.pools.PoolOfKey(ctx, key.Row)
	return h.pools.Offers(ctx, poolID, poolsModelOf(routingToCatalogEntry(entry)), h.now().UnixMilli())
}

func (h poolHooks) RecordTraffic(key *gateway.Key, accountID string, success bool, tokens int64) {
	if key == nil || key.Row == nil {
		return
	}
	h.pools.RecordFinished(context.Background(), key.Row, accountID, success, tokens)
}

func (h poolHooks) NoteReachability(accountID string, success bool, kind string) {
	now := h.now().UnixMilli()
	if success {
		h.pools.NotePartnerSuccess(accountID)
		return
	}
	h.pools.NotePartnerFailure(accountID, providers.Kind(kind), now)
}

// mediaSearch runs one web search for chat's web-search tool through the media
// capability's shared pipeline.
func mediaSearch(svc *media.Service) func(ctx context.Context, call gateway.SearchCall) (*providers.SearchResult, error) {
	return func(ctx context.Context, call gateway.SearchCall) (*providers.SearchResult, error) {
		var row *routerkeys.Row
		var request *http.Request
		if call.Key != nil {
			row = call.Key.Row
		}
		answer, err := svc.SearchWeb(ctx, request, row, media.SearchInput{
			Query:          call.Query,
			MaxResults:     call.MaxResults,
			IncludeDomains: call.IncludeDomains,
			ExcludeDomains: call.ExcludeDomains,
		}, call.FreeOnly, "web_search_tool")
		if err != nil {
			return nil, err
		}
		out := &providers.SearchResult{}
		for _, hit := range answer.Results {
			out.Results = append(out.Results, providers.SearchHit{
				Title:     hit.Title,
				URL:       hit.URL,
				Snippet:   hit.Snippet,
				Content:   hit.Content,
				Published: hit.Published,
			})
		}
		return out, nil
	}
}

// answerForRequest finds the successful answer of one key (the requests row
// POST /v1/feedback names).
func (d *deps) answerForRequest(requestID, keyID string) (*gateway.Answer, error) {
	var modelRef int64
	var modelLabel, task *string
	err := d.db.R.QueryRow(
		`SELECT model_ref, model_label, task FROM requests WHERE id = ? AND router_key_id = ? AND status = 'success'`,
		requestID, keyID,
	).Scan(&modelRef, &modelLabel, &task)
	if err != nil {
		return nil, nil
	}
	answer := &gateway.Answer{ModelRef: modelRef}
	if modelLabel != nil {
		answer.ModelLabel = *modelLabel
	}
	if task != nil {
		answer.Task = *task
	}
	return answer, nil
}

// shieldTexts is the privacy shield for text that leaves without an answer to
// put values back into (embeddings, image prompts, direct searches).
func mediaShield(svc *processing.Service) media.Shield {
	return func(userID string, texts []string) (media.Shielded, error) {
		shielded, redacted := svc.ShieldTexts(context.Background(), userID, texts)
		return media.Shielded{Texts: shielded, Redacted: redacted}, nil
	}
}

// poolMinimum is the pools domain's withPoolMinimum: the shield settings raised
// to what a request that may reach a partner needs.
func poolMinimum(settings processing.PrivacyConfig) processing.PrivacyConfig {
	converted := pools.PrivacyConfig{
		Enabled:     settings.Enabled,
		Modes:       settings.Modes,
		CustomTerms: termsOf(settings.CustomTerms),
		AllowTerms:  termsOf(settings.AllowTerms),
	}
	raised := pools.WithPoolMinimum(converted)
	out := settings
	out.Enabled = raised.Enabled
	out.Modes = raised.Modes
	out.CustomTerms = stringsOf(raised.CustomTerms)
	out.AllowTerms = stringsOf(raised.AllowTerms)
	return out
}

func termsOf(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func stringsOf(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// spaceOf resolves the request's space for the domains that only want its owner.
func spaceOwner(ctx context.Context) (string, bool) {
	space, ok := teams.SpaceFromContext(ctx)
	if !ok || space == nil {
		return "", false
	}
	return space.OwnerID, true
}

// mediaSpace is the request's space in the shape the media routes read.
func mediaSpace(ctx context.Context) (media.Space, bool) {
	space, ok := teams.SpaceFromContext(ctx)
	if !ok || space == nil {
		return media.Space{}, false
	}
	return media.Space{OwnerID: space.OwnerID, ActorID: space.ActorID}, true
}

// gatewaySpace is the request's space in the shape the gateway's playground
// route reads.
func gatewaySpace(ctx context.Context) (gateway.Space, bool) {
	owner, ok := spaceOwner(ctx)
	if !ok {
		return gateway.Space{}, false
	}
	return gateway.Space{OwnerID: owner}, true
}

// processingSpace is the request's space in the shape the processing routes
// read.
func processingSpace(ctx context.Context) (processing.Space, bool) {
	space, ok := teams.SpaceFromContext(ctx)
	if !ok || space == nil {
		return processing.Space{}, false
	}
	return processing.Space{OwnerID: space.OwnerID, Permissions: space.Permissions}, true
}

// catalogDefinitions is the provider registry as the catalog's sync reads a
// definition.
func catalogLookup(svc *catalog.Service) func(ctx context.Context, userID, capability string, includeUnavailable bool) ([]catalog.Entry, error) {
	return func(ctx context.Context, userID, capability string, includeUnavailable bool) ([]catalog.Entry, error) {
		return svc.ListForUser(ctx, userID, capability, includeUnavailable)
	}
}

var _ = fmt.Sprintf
