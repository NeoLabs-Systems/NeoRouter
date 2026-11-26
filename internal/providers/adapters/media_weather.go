package adapters

import (
	"context"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Weather services behind one method: weather({ location, current, hourly,
// daily, units, days }) → one normalized forecast, whichever service answered.
// `location` is a place name or { latitude, longitude }; units are 'metric'
// (°C, km/h, mm) or 'imperial' (°F, mph, inch). Every service speaks its own
// dialect; the definition names it (`weather.service`).
//
// The normalized blocks are structs, not maps, so their keys keep a stable
// order when the capability marshals them.

// mediaWeatherParams is one lookup's arguments.
type mediaWeatherParams struct {
	Location any
	Current  bool
	Hourly   bool
	Daily    bool
	Units    string
	Days     int
}

// mediaWeatherService is one service's dialect.
type mediaWeatherService func(ctx context.Context, adapter *WeatherAdapter, params mediaWeatherParams, call providers.Call) (*providers.WeatherReport, error)

// mediaWeatherServices are the dialects a definition may name.
var mediaWeatherServices = map[string]mediaWeatherService{
	"open_meteo":      weatherOpenMeteo,
	"openweathermap":  weatherOpenWeatherMap,
	"weatherapi":      weatherWeatherAPI,
	"tomorrow":        weatherTomorrow,
	"visual_crossing": weatherVisualCrossing,
}

// WeatherAdapter is one weather service account.
type WeatherAdapter struct {
	*mediaBase
}

// NewWeatherAdapter builds the weather adapter.
func NewWeatherAdapter(def providers.Definition, deps providers.AdapterDeps) *WeatherAdapter {
	return &WeatherAdapter{mediaBase: newMediaBase(def, deps)}
}

// serviceName is the dialect this account speaks.
func (w *WeatherAdapter) serviceName() string {
	if w.def.Weather == nil {
		return ""
	}
	return w.def.Weather.Service
}

// Supports reports the one method a weather service serves.
func (w *WeatherAdapter) Supports(method providers.Capability) bool {
	return method == providers.CapWeather
}

// TranscribeTranslate is true: a weather service never transcribes, and the
// default is "can translate".
func (w *WeatherAdapter) TranscribeTranslate() bool { return true }

// TranscribeTimestamps is true for a provider that never transcribes.
func (w *WeatherAdapter) TranscribeTimestamps() bool { return true }

// Validate does a real current-weather lookup for fixed coordinates: one call
// of quota.
func (w *WeatherAdapter) Validate(ctx context.Context) (providers.Validation, error) {
	_, err := w.weather(ctx, providers.WeatherRequest{
		Location: map[string]any{"latitude": 52.52, "longitude": 13.41},
		Current:  true,
	}, providers.Call{Timeout: 20 * time.Second})
	if err != nil {
		classified := providers.AsProviderError(err)
		if classified == nil {
			return providers.UnknownKey(err.Error()), nil
		}
		if classified.Kind == providers.KindAuth || classified.Kind == providers.KindPayment {
			return providers.InvalidKey(classified.Message), nil
		}
		return providers.UnknownKey(classified.Message), nil
	}
	return providers.ValidKey(), nil
}

// ListModels is the definition's fixed listing: one weather model.
func (w *WeatherAdapter) ListModels(context.Context) ([]providers.Model, error) {
	return w.staticModels(nil), nil
}

// FetchQuota reports nothing: the services publish no quota endpoint.
func (w *WeatherAdapter) FetchQuota(context.Context) ([]providers.QuotaWindow, error) {
	return nil, nil
}

// Weather looks a forecast up. `units` is left out of the report: the media
// capability reads result.units as the billed units.
func (w *WeatherAdapter) Weather(ctx context.Context, req providers.WeatherRequest) (*providers.WeatherReport, error) {
	return w.weather(ctx, req, providers.Call{})
}

// weather is Weather with the call's own timeout, for Validate.
func (w *WeatherAdapter) weather(ctx context.Context, req providers.WeatherRequest, call providers.Call) (*providers.WeatherReport, error) {
	service, found := mediaWeatherServices[w.serviceName()]
	if !found {
		return nil, providers.NewError(providers.KindBadRequest, "Unknown weather service "+w.serviceName()+".", providers.ErrorOptions{})
	}
	units := req.Units
	if units == "" {
		units = "metric"
	}
	days := req.Days
	if days == 0 {
		days = 3
	}
	if days < 1 {
		days = 1
	}
	if days > 16 {
		days = 16
	}
	return service(ctx, w, mediaWeatherParams{
		Location: req.Location, Current: req.Current, Hourly: req.Hourly, Daily: req.Daily,
		Units: units, Days: days,
	}, call)
}
