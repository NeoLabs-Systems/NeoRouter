package app

// The providers domain's seams: the provider definition registry's adapter
// factory (chat dialects + media dialects in one), the model listing the catalog
// syncs from, the account limits/quota/router-key records the other domains
// write through, and the SOCKS pool the provider HTTP layer sends public calls
// through.

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/secretbox"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/localmodels"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/providers/adapters"
	"github.com/NeoLabs-Systems/neorouter/internal/proxies"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
)

// ensureSecretKey reads secret.key, creating it with 32 random bytes and 0600
// permissions when it is missing, exactly like crypto.js masterKey().
func ensureSecretKey(path string) ([]byte, error) {
	key, err := providers.EnsureSecretKey(path)
	if err != nil {
		return nil, fmt.Errorf("app: secret key: %w", err)
	}
	if len(key) != providers.SecretKeySize {
		return nil, providers.ErrSecretKeySize
	}
	return key, nil
}

// newSealer builds the sealer every domain that stores secrets uses:
// secretbox keyed on hex(secret.key), legacy values still readable.
func newSealer(key []byte) (*providers.Sealer, error) {
	sealer, err := providers.NewSealerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("app: build secret sealer: %w", err)
	}
	return sealer, nil
}

// boxOf is the secretbox the settings domain seals secrets with.
func boxOf(key []byte) (*secretbox.Box, error) {
	box, err := secretbox.New(providers.SealedPrefix, hex.EncodeToString(key))
	if err != nil {
		return nil, fmt.Errorf("app: build secret box: %w", err)
	}
	return box, nil
}

// proxySealer is the sealer of stored proxy URLs (secretbox for writes, the
// legacy raw-key scheme for values the Node server wrote).
func proxySealer(key []byte) *proxies.Sealer {
	box, err := boxOf(key)
	if err != nil {
		return proxies.NewSealer(nil, nil)
	}
	return proxies.NewSealer(box, key)
}

// persistSecret stores a rotated OAuth secret on a connection
// (account_service.storeSecret): providers renew the refresh token on every
// refresh, so a refresh that is not stored leaves the saved connection with a
// dead one.
func (d *deps) persistSecret(accountID, secret string) error {
	if d.Accounts == nil || d.sealer == nil {
		return nil
	}
	sealed, err := d.sealer.Seal(secret)
	if err != nil {
		return err
	}
	if _, err := d.db.W.Exec(`UPDATE provider_accounts SET secret_encrypted = ? WHERE id = ?`, sealed, accountID); err != nil {
		return fmt.Errorf("app: store refreshed secret: %w", err)
	}
	return nil
}

// mergedFactory builds one providers.AdapterFactory out of the chat dialects and
// the media dialects, the way adapters/index.js tried both before failing.
type mergedFactory struct {
	factory *adapters.Factory
}

func (m mergedFactory) build(def providers.Definition, deps providers.AdapterDeps) (providers.Adapter, error) {
	adapter, err := m.factory.NewAdapter(def, deps)
	if err == nil {
		return adapter, nil
	}
	if !errors.Is(err, adapters.ErrNotChatAdapter) {
		return nil, err
	}
	media, ok, mediaErr := adapters.NewMediaAdapter(def, deps)
	if mediaErr != nil {
		return nil, mediaErr
	}
	if !ok {
		return nil, fmt.Errorf("providers: no adapter for definition %q", def.ID)
	}
	return media, nil
}

// definitionsOf is the provider registry as the catalog reads it
// (registry.get, reduced to what the catalog needs).
type definitionsOf struct {
	registry *providers.Registry
}

func (d definitionsOf) Definition(id string) (catalog.Definition, bool) {
	def, ok := d.registry.Get(id)
	if !ok {
		return catalog.Definition{}, false
	}
	trust := def.Billing.TrustListingFree
	return catalog.Definition{
		ID:       def.ID,
		Label:    def.Label,
		Category: string(def.Category),
		BaseURL:  def.BaseURL,
		Quirks:   catalog.Quirks{NoTools: def.Quirks.NoTools != nil && *def.Quirks.NoTools},
		Billing: catalog.Billing{
			FreeTierAll:      def.Billing.FreeTierAll,
			FreeTierModels:   def.Billing.FreeTierModels,
			AlwaysFree:       def.Billing.AlwaysFree,
			Verify:           def.Billing.Verify != nil,
			LocalOnly:        def.Billing.LocalOnly,
			KeylessOnly:      def.Billing.KeylessOnly,
			TrustListingFree: &trust,
		},
		ModelFacts: modelFactsOf(def.Models.Facts),
	}, true
}

// modelFactsOf adapts the documented per-model limits of a definition to the
// catalog's view, nil when the definition documents none.
func modelFactsOf(facts map[string]providers.ModelFacts) map[string]catalog.ModelFacts {
	if len(facts) == 0 {
		return nil
	}
	adapted := make(map[string]catalog.ModelFacts, len(facts))
	for modelID, entry := range facts {
		adapted[modelID] = catalog.ModelFacts{ContextWindow: entry.ContextWindow, MaxOutput: entry.MaxOutput}
	}
	return adapted
}

// accountModels lists a connection's models for a catalog sync
// (account_service.getRuntime + adapter.listModels).
type accountModels struct {
	accounts *providers.Service
	registry *providers.Registry
}

func (m accountModels) ListModels(ctx context.Context, accountID string) ([]catalog.ListedModel, bool, error) {
	runtime, err := m.accounts.Runtime(ctx, accountID)
	if err != nil {
		return nil, false, err
	}
	if runtime == nil || !runtime.Enabled {
		return nil, false, nil
	}
	adapter, err := m.accounts.AdapterFor(ctx, runtime)
	if err != nil {
		return nil, false, err
	}
	if adapter == nil {
		return nil, false, nil
	}
	listed, err := adapter.ListModels(ctx)
	if err != nil {
		return nil, false, err
	}
	out := make([]catalog.ListedModel, 0, len(listed))
	for _, model := range listed {
		out = append(out, listedModel(model))
	}
	return out, true, nil
}

// listedModel is providers.Model as the catalog records it.
func listedModel(model providers.Model) catalog.ListedModel {
	out := catalog.ListedModel{
		ID:              model.ID,
		Name:            model.Name,
		Kind:            model.Kind,
		PriceKnown:      model.PriceKnown,
		PriceIn:         model.PriceIn,
		PriceOut:        model.PriceOut,
		PriceCachedIn:   model.PriceCachedIn,
		PriceCacheWrite: model.PriceCacheWrite,
		FreeHint:        model.FreeHint,
		PaidOnly:        model.PaidOnly,
		Tools:           model.Tools,
		Vision:          model.Vision,
		JSON:            model.JSON,
		Reasoning:       model.Reasoning,
		Decision:        model.Decision,
		UnitPrice:       model.UnitPrice,
		OptIn:           optIn(model.Raw),
	}
	out.ContextWindow = int64Ptr(model.ContextWindow)
	out.MaxOutput = int64Ptr(model.MaxOutput)
	if model.PriceUnit != nil {
		out.PriceUnit = *model.PriceUnit
	}
	return out
}

// optIn is a listing entry's optIn flag (catalog_service reads it off the raw
// model record).
func optIn(raw map[string]any) bool {
	if raw == nil {
		return false
	}
	value, ok := raw["optIn"].(bool)
	return ok && value
}

func int64Ptr(value *int) *int64 {
	if value == nil {
		return nil
	}
	converted := int64(*value)
	return &converted
}

// accountCatalog is the catalog as the providers domain consumes it when a
// connection is created or its traffic is reclassified.
type accountCatalog struct {
	svc *catalog.Service
}

func (c accountCatalog) SyncAccount(ctx context.Context, accountID string) (any, error) {
	return c.svc.SyncAccount(ctx, accountID)
}

func (c accountCatalog) ReclassifyAccount(ctx context.Context, accountID string) error {
	return c.svc.ReclassifyAccount(ctx, accountID)
}

func (c accountCatalog) FlagModel(ctx context.Context, ref int64, flags map[string]any) (bool, error) {
	return c.svc.FlagModel(ctx, ref, flags)
}

// quotaRecorder is the quota store as the providers domain writes allowance
// observations through.
type quotaRecorder struct {
	svc *quota.Service
}

func (r quotaRecorder) SetState(scope, metric string, obs providers.QuotaObservation, source string, now time.Time) error {
	return r.svc.SetState(scope, metric, quota.Observation{
		Metric:    metric,
		Limit:     obs.Limit,
		Remaining: obs.Remaining,
		ResetAt:   obs.ResetAt,
	}, source, now)
}

// quotaDefinitions is the provider registry as the quota domain reads the
// curated limits of a definition.
type quotaDefinitions struct {
	registry *providers.Registry
}

func (q quotaDefinitions) QuotaDefinition(definitionID string) *quota.Definition {
	def, ok := q.registry.Get(definitionID)
	if !ok {
		return nil
	}
	limits := quota.Limits{
		Pool:      def.Limits.Pool,
		PoolMatch: def.Limits.PoolMatch,
		Defaults:  limitValues(def.Limits.Defaults),
		Account:   limitValues(def.Limits.Account),
		Models:    make([]quota.ModelLimit, 0, len(def.Limits.Models)),
	}
	for _, limit := range def.Limits.Models {
		limits.Models = append(limits.Models, quota.ModelLimit{
			Match:  limit.Match,
			Values: limitValues(limit.LimitSpec),
		})
	}
	return &quota.Definition{
		Limits: limits,
		Header: quota.HeaderConfig{
			HeaderPeriods: map[string]string{
				"requests": def.Quota.HeaderPeriods.Requests,
				"tokens":   def.Quota.HeaderPeriods.Tokens,
			},
			PlainHeaderMetric: def.Quota.PlainHeaderMetric,
		},
	}
}

// limitValues turns a definition's stated limits into the metric map the quota
// domain works with; an unstated metric stays absent.
func limitValues(spec providers.LimitSpec) map[string]float64 {
	values := map[string]float64{}
	add := func(metric string, value int) {
		if value > 0 {
			values[metric] = float64(value)
		}
	}
	add(quota.RPM, spec.RPM)
	add(quota.RPD, spec.RPD)
	add(quota.TPM, spec.TPM)
	add(quota.TPD, spec.TPD)
	add("concurrent", spec.Concurrent)
	return values
}

// proxyPool is the outbound SOCKS pool as the provider HTTP layer uses it.
type proxyPool struct {
	svc *proxies.Service
}

func (p proxyPool) Enabled() bool {
	if p.svc == nil {
		return false
	}
	policy, err := p.svc.PoolPolicy(context.Background())
	return err == nil && policy.Enabled
}

func (p proxyPool) Do(req *http.Request) (*http.Response, error) {
	return p.svc.Fetch(req.Context(), req)
}

// localModelsAdapter is the local-model service as the provider adapters read
// it. The service is built late (it needs settings, processing and the
// connections it shares), so the adapter holds a pointer filled in at the end of
// build.
type localModelsAdapter struct {
	svc *localmodels.Service
}

func (a localModelsAdapter) Allowed(ctx context.Context, userID string) bool {
	if a.svc == nil {
		return false
	}
	return a.svc.Access.Allowed(ctx, userID)
}

// Served are the models answering right now, with what they can do.
func (a localModelsAdapter) Served() []adapters.LocalModel {
	if a.svc == nil {
		return nil
	}
	served := a.svc.ServedModels()
	out := make([]adapters.LocalModel, 0, len(served))
	for _, model := range served {
		if model.Entry == nil {
			continue
		}
		entry := model.Entry
		item := adapters.LocalModel{
			ID:        entry.ID,
			Name:      entry.Name,
			Kind:      entry.Kind,
			Tools:     entry.Features.Tools,
			Vision:    entry.Features.Vision,
			Reasoning: entry.Features.Reasoning,
			Translate: entry.Features.Translate,
		}
		if model.ContextWindow != nil {
			item.ContextWindow = *model.ContextWindow
		}
		for _, voice := range entry.Voices {
			item.Voices = append(item.Voices, providers.Voice{ID: voice.ID, Name: voice.Name})
		}
		out = append(out, item)
	}
	return out
}

// Running is the running instance of one model, in the shape the local runtime
// adapter speaks.
func (a localModelsAdapter) Running(modelID string) (adapters.LocalInstance, bool) {
	if a.svc == nil {
		return adapters.LocalInstance{}, false
	}
	instance := a.svc.RunningInstance(modelID)
	if instance == nil {
		return adapters.LocalInstance{}, false
	}
	out := adapters.LocalInstance{}
	if instance.Endpoint != nil {
		out.Endpoint = &adapters.LocalEndpoint{BaseURL: instance.Endpoint.BaseURL, Token: instance.Endpoint.Token}
	}
	if instance.Invoke != nil {
		invoke := instance.Invoke
		out.Speech = func(ctx context.Context, in adapters.LocalSpeechInput) (adapters.LocalSpeechOutput, error) {
			speed := float64(0)
			if in.Speed != nil {
				speed = *in.Speed
			}
			answer, err := invoke(ctx, "speech", localmodels.SpeechRequest{Text: in.Text, Voice: in.Voice, Speed: speed})
			if err != nil {
				return adapters.LocalSpeechOutput{}, err
			}
			result, ok := answer.(localmodels.SpeechResult)
			if !ok {
				return adapters.LocalSpeechOutput{}, fmt.Errorf("local models: unexpected speech answer %T", answer)
			}
			return adapters.LocalSpeechOutput{Samples: result.Samples, Rate: result.Rate}, nil
		}
		out.Transcribe = func(ctx context.Context, in adapters.LocalTranscribeInput) (adapters.LocalTranscribeOutput, error) {
			samples, ok := localmodels.ToSamples(in.File)
			if !ok {
				return adapters.LocalTranscribeOutput{}, fmt.Errorf("local models: the audio could not be decoded")
			}
			answer, err := invoke(ctx, "transcribe", localmodels.TranscribeRequest{
				Samples:   samples,
				Language:  in.Language,
				Translate: in.Translate,
			})
			if err != nil {
				return adapters.LocalTranscribeOutput{}, err
			}
			result, ok := answer.(localmodels.Transcription)
			if !ok {
				return adapters.LocalTranscribeOutput{}, fmt.Errorf("local models: unexpected transcription answer %T", answer)
			}
			out := adapters.LocalTranscribeOutput{Text: result.Text, Language: result.Language}
			if result.Duration > 0 {
				duration := result.Duration
				out.Duration = &duration
			}
			for _, segment := range result.Segments {
				end := segment.Start
				if segment.End != nil {
					end = *segment.End
				}
				out.Segments = append(out.Segments, providers.Segment{Start: segment.Start, End: end, Text: segment.Text})
			}
			return out, nil
		}
	}
	return out, true
}

// localPolicy is the sharing policy the provider service reads: admins always
// may use the local models, everyone else only when an admin shares them.
type localPolicy struct {
	adapter *localModelsAdapter
}

func (p localPolicy) Allowed(userID string) bool {
	return p.adapter.Allowed(context.Background(), userID)
}

func (p localPolicy) Offered(userID string) bool {
	if p.adapter.svc == nil {
		return false
	}
	return p.adapter.svc.Access.Offered(context.Background(), userID)
}

var _ = config.Config{}
