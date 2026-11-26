package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The weather dialects against a fake upstream: what each service is asked for
// and the one normalized forecast they all answer with. Tests of the weather
// adapter, driven over HTTP.

// mediaWeatherFake answers a weather service from a table of path prefixes.
type mediaWeatherFake struct {
	answers map[string]any
	seen    []string
	missing []string
}

// handler answers by path prefix and records what was asked for.
func (f *mediaWeatherFake) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.seen = append(f.seen, r.URL.String())
		for prefix, answer := range f.answers {
			if strings.HasPrefix(r.URL.Path, prefix) {
				mediaTestJSON(t, w, answer)
				return
			}
		}
		f.missing = append(f.missing, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no such endpoint"}`))
	}
}

// mediaWeatherAdapter builds a weather adapter of one definition against baseURL.
func mediaWeatherAdapter(t *testing.T, id, baseURL string) *WeatherAdapter {
	t.Helper()
	return NewWeatherAdapter(mediaTestDefinition(t, id), mediaTestDeps("k", baseURL, nil))
}

// mediaWeatherField reads one field of a report block through JSON, so the test
// sees exactly what a client would.
func mediaWeatherField(t *testing.T, block any, field string) any {
	t.Helper()
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("encode block: %v", err)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode block: %v", err)
	}
	return decoded[field]
}

// mediaWeatherNumber reads a numeric field.
func mediaWeatherNumber(t *testing.T, block any, field string) float64 {
	t.Helper()
	value, ok := mediaWeatherField(t, block, field).(float64)
	if !ok {
		t.Fatalf("%s = %v", field, mediaWeatherField(t, block, field))
	}
	return value
}

// TestWeatherDefinitionsAreFreeByDefault: every service is free until proven
// otherwise, with the day it was checked.
func TestWeatherDefinitionsAreFreeByDefault(t *testing.T) {
	registry := mediaTestRegistry(t)
	for _, id := range []string{"open_meteo", "openweathermap", "weatherapi", "tomorrow_io", "visual_crossing"} {
		def, ok := registry.Get(id)
		if !ok {
			t.Fatalf("%s is missing", id)
		}
		if def.Adapter != providers.AdapterWeather {
			t.Fatalf("%s adapter = %q", id, def.Adapter)
		}
		if def.Billing.DefaultMode != providers.BillingFreeTier || def.Billing.CheckedAt == "" {
			t.Fatalf("%s billing = %+v", id, def.Billing)
		}
		adapter, built, err := NewMediaAdapter(def, mediaTestDeps("k", "", nil))
		if err != nil || !built || adapter == nil {
			t.Fatalf("%s did not build: %v", id, err)
		}
	}
}

// TestOpenWeatherMapFoldsThreeHourStepsIntoDays: geocoding, m/s to km/h and the
// days a forecast's steps fall in.
func TestOpenWeatherMapFoldsThreeHourStepsIntoDays(t *testing.T) {
	fake := &mediaWeatherFake{answers: map[string]any{
		"/geo/1.0/direct": []any{map[string]any{"name": "Berlin", "country": "DE", "lat": 52.5, "lon": 13.4}},
		"/data/2.5/weather": map[string]any{
			"dt": 1793361600, "timezone": 3600,
			"main":    map[string]any{"temp": 10, "feels_like": 8, "humidity": 80},
			"wind":    map[string]any{"speed": 5, "deg": 200},
			"weather": []any{map[string]any{"description": "light rain"}},
			"rain":    map[string]any{"1h": 0.5},
		},
		"/data/2.5/forecast": map[string]any{
			"city": map[string]any{"timezone": 3600},
			"list": []any{
				map[string]any{"dt": 1793361600, "main": map[string]any{"temp": 10, "temp_max": 11, "temp_min": 9}, "wind": map[string]any{"speed": 2}, "pop": 0.2, "weather": []any{map[string]any{"description": "clouds"}}},
				map[string]any{"dt": 1793372400, "main": map[string]any{"temp": 12, "temp_max": 13, "temp_min": 12}, "wind": map[string]any{"speed": 2}, "pop": 0.6, "rain": map[string]any{"3h": 1.5}, "weather": []any{map[string]any{"description": "rain"}}},
			},
		},
	}}
	base := mediaTestServer(t, fake.handler(t))
	adapter := mediaWeatherAdapter(t, "openweathermap", base)
	report, err := adapter.Weather(context.Background(), providers.WeatherRequest{
		Location: "Berlin", Current: true, Hourly: true, Daily: true,
	})
	if err != nil {
		t.Fatalf("weather: %v", err)
	}
	if len(fake.missing) > 0 {
		t.Fatalf("unexpected endpoints: %v", fake.missing)
	}
	if got := mediaWeatherField(t, report.Location, "name"); got != "Berlin, DE" {
		t.Fatalf("name = %v", got)
	}
	if got := mediaWeatherNumber(t, report.Current, "wind_speed"); got != 18 {
		t.Fatalf("wind speed = %v", got)
	}
	if got := mediaWeatherField(t, report.Current, "condition"); got != "light rain" {
		t.Fatalf("condition = %v", got)
	}
	hours, _ := report.Hourly.([]MediaWeatherHour)
	if len(hours) != 2 {
		t.Fatalf("hours = %d", len(hours))
	}
	days, _ := report.Daily.([]MediaWeatherDay)
	if len(days) != 1 {
		t.Fatalf("days = %d", len(days))
	}
	for field, want := range map[string]float64{"temperature_max": 13, "temperature_min": 9, "precipitation_probability": 60} {
		if got := mediaWeatherNumber(t, days[0], field); got != want {
			t.Fatalf("%s = %v, want %v", field, got, want)
		}
	}
	for _, path := range fake.seen {
		if !strings.Contains(path, "appid=k") {
			t.Fatalf("a call without the key: %s", path)
		}
	}
}

// TestWeatherAPIPicksTheImperialFields: WeatherAPI.com sends both units, and the
// request picks which one to read.
func TestWeatherAPIPicksTheImperialFields(t *testing.T) {
	fake := &mediaWeatherFake{answers: map[string]any{
		"/v1/forecast.json": map[string]any{
			"location": map[string]any{"name": "Paris", "country": "France", "lat": 48.8, "lon": 2.3, "tz_id": "Europe/Paris"},
			"current": map[string]any{
				"last_updated": "2026-10-30 12:00", "temp_c": 10, "temp_f": 50,
				"feelslike_c": 9, "feelslike_f": 48, "humidity": 70,
				"wind_kph": 10, "wind_mph": 6.2, "wind_degree": 90,
				"precip_mm": 0, "precip_in": 0,
				"condition": map[string]any{"text": "Sunny"},
			},
			"forecast": map[string]any{"forecastday": []any{map[string]any{
				"date": "2026-10-30",
				"day": map[string]any{
					"maxtemp_c": 12, "maxtemp_f": 53.6, "mintemp_c": 5, "mintemp_f": 41,
					"totalprecip_mm": 0, "totalprecip_in": 0,
					"daily_chance_of_rain": 10, "daily_chance_of_snow": 0,
					"condition": map[string]any{"text": "Sunny"},
				},
				"astro": map[string]any{"sunrise": "08:20 AM", "sunset": "05:40 PM"},
				"hour":  []any{},
			}}},
		},
	}}
	base := mediaTestServer(t, fake.handler(t))
	adapter := mediaWeatherAdapter(t, "weatherapi", base+"/v1")
	report, err := adapter.Weather(context.Background(), providers.WeatherRequest{
		Location: map[string]any{"latitude": 48.8, "longitude": 2.3},
		Current:  true, Daily: true, Units: "imperial",
	})
	if err != nil {
		t.Fatalf("weather: %v", err)
	}
	if got := mediaWeatherNumber(t, report.Current, "temperature"); got != 50 {
		t.Fatalf("temperature = %v", got)
	}
	days, _ := report.Daily.([]MediaWeatherDay)
	if got := mediaWeatherNumber(t, days[0], "temperature_max"); got != 53.6 {
		t.Fatalf("max = %v", got)
	}
	if got := mediaWeatherField(t, report.Location, "timezone"); got != "Europe/Paris" {
		t.Fatalf("timezone = %v", got)
	}
	if !strings.Contains(fake.seen[0], "q=48.8%2C2.3") {
		t.Fatalf("query = %s", fake.seen[0])
	}
}

// TestTomorrowAndVisualCrossingMapToTheSameShape: two dialects, one forecast.
func TestTomorrowAndVisualCrossingMapToTheSameShape(t *testing.T) {
	tomorrow := &mediaWeatherFake{answers: map[string]any{
		"/v4/weather/realtime": map[string]any{
			"data": map[string]any{
				"time": "2026-10-30T12:00:00Z",
				"values": map[string]any{
					"temperature": 10, "temperatureApparent": 9, "humidity": 60,
					"windSpeed": 5, "windDirection": 180, "weatherCode": 1001,
				},
			},
			"location": map[string]any{"lat": 1, "lon": 2, "name": "X"},
		},
		"/v4/weather/forecast": map[string]any{
			"timelines": map[string]any{"daily": []any{map[string]any{
				"time": "2026-10-30T06:00:00Z",
				"values": map[string]any{
					"temperatureMax": 12, "temperatureMin": 4,
					"precipitationProbabilityMax": 20, "weatherCodeMax": 4001,
				},
			}}},
		},
	}}
	base := mediaTestServer(t, tomorrow.handler(t))
	report, err := mediaWeatherAdapter(t, "tomorrow_io", base+"/v4").Weather(context.Background(), providers.WeatherRequest{
		Location: "X", Current: true, Daily: true,
	})
	if err != nil {
		t.Fatalf("weather: %v", err)
	}
	if got := mediaWeatherNumber(t, report.Current, "wind_speed"); got != 18 {
		t.Fatalf("wind speed = %v", got)
	}
	if got := mediaWeatherField(t, report.Current, "condition"); got != "Cloudy" {
		t.Fatalf("condition = %v", got)
	}
	days, _ := report.Daily.([]MediaWeatherDay)
	if got := mediaWeatherField(t, days[0], "condition"); got != "Rain" {
		t.Fatalf("daily condition = %v", got)
	}

	visual := &mediaWeatherFake{answers: map[string]any{
		"/VisualCrossingWebServices/rest/services/timeline/": map[string]any{
			"resolvedAddress": "Rome", "latitude": 41.9, "longitude": 12.5, "timezone": "Europe/Rome",
			"currentConditions": map[string]any{
				"datetime": "12:00:00", "temp": 18, "feelslike": 18, "humidity": 50,
				"windspeed": 8, "winddir": 10, "conditions": "Clear",
			},
			"days": []any{map[string]any{
				"datetime": "2026-10-30", "tempmax": 20, "tempmin": 11, "precip": 0,
				"precipprob": 0, "conditions": "Clear", "sunrise": "07:00:00", "sunset": "17:10:00",
			}},
		},
	}}
	base = mediaTestServer(t, visual.handler(t))
	report, err = mediaWeatherAdapter(t, "visual_crossing", base+"/VisualCrossingWebServices/rest/services").Weather(context.Background(), providers.WeatherRequest{
		Location: "Rome", Current: true, Daily: true, Days: 1,
	})
	if err != nil {
		t.Fatalf("weather: %v", err)
	}
	if got := mediaWeatherField(t, report.Current, "time"); got != "2026-10-30T12:00:00" {
		t.Fatalf("time = %v", got)
	}
	days, _ = report.Daily.([]MediaWeatherDay)
	if got := mediaWeatherNumber(t, days[0], "temperature_max"); got != 20 {
		t.Fatalf("max = %v", got)
	}
	if !strings.HasPrefix(visual.seen[0], "/VisualCrossingWebServices/rest/services/timeline/Rome/next0days?") {
		t.Fatalf("path = %s", visual.seen[0])
	}
}

// TestOpenMeteoGeocodesAndAsksForBothUnits: the WMO condition, the timezone and
// the unit parameters.
func TestOpenMeteoGeocodesAndAsksForBothUnits(t *testing.T) {
	geocoding := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "Berlin" {
			t.Errorf("name = %q", r.URL.Query().Get("name"))
		}
		mediaTestJSON(t, w, map[string]any{"results": []any{map[string]any{
			"name": "Berlin", "admin1": "Berlin", "country": "Germany", "latitude": 52.52, "longitude": 13.41,
		}}})
	})
	previous := mediaOpenMeteoGeocoding
	mediaOpenMeteoGeocoding = geocoding + "/search"
	t.Cleanup(func() { mediaOpenMeteoGeocoding = previous })

	fake := &mediaWeatherFake{answers: map[string]any{
		"/v1/forecast": map[string]any{
			"timezone": "Europe/Berlin",
			"current": map[string]any{
				"time": "2026-10-30T12:00", "temperature_2m": 11.2, "apparent_temperature": 9.8,
				"relative_humidity_2m": 71, "wind_speed_10m": 14.4, "wind_direction_10m": 240,
				"precipitation": 0, "weather_code": 3,
			},
			"daily": map[string]any{
				"time": []any{"2026-10-30"}, "temperature_2m_max": []any{12.5}, "temperature_2m_min": []any{6.1},
				"precipitation_sum": []any{0.4}, "precipitation_probability_max": []any{35},
				"weather_code": []any{61}, "sunrise": []any{"2026-10-30T07:05"}, "sunset": []any{"2026-10-30T16:38"},
			},
		},
	}}
	base := mediaTestServer(t, fake.handler(t))
	adapter := mediaWeatherAdapter(t, "open_meteo", base+"/v1")
	report, err := adapter.Weather(context.Background(), providers.WeatherRequest{Location: "Berlin", Current: true, Daily: true})
	if err != nil {
		t.Fatalf("weather: %v", err)
	}
	if got := mediaWeatherField(t, report.Location, "name"); got != "Berlin, Berlin, Germany" {
		t.Fatalf("name = %v", got)
	}
	if got := mediaWeatherField(t, report.Location, "timezone"); got != "Europe/Berlin" {
		t.Fatalf("timezone = %v", got)
	}
	if got := mediaWeatherField(t, report.Current, "condition"); got != "Overcast" {
		t.Fatalf("condition = %v", got)
	}
	days, _ := report.Daily.([]MediaWeatherDay)
	if got := mediaWeatherField(t, days[0], "condition"); got != "Light rain" {
		t.Fatalf("daily condition = %v", got)
	}
	if got := mediaWeatherField(t, days[0], "sunrise"); got != "2026-10-30T07:05" {
		t.Fatalf("sunrise = %v", got)
	}

	// Imperial units are the same forecast in other units, asked for by parameter.
	if _, err := adapter.Weather(context.Background(), providers.WeatherRequest{Location: "Berlin", Current: true}); err != nil {
		t.Fatalf("weather: %v", err)
	}
	if _, err := adapter.Weather(context.Background(), providers.WeatherRequest{Location: "Berlin", Current: true, Units: "imperial"}); err != nil {
		t.Fatalf("weather: %v", err)
	}
	query := mediaQueryOf(t, fake.seen[len(fake.seen)-1])
	for parameter, want := range map[string]string{"temperature_unit": "fahrenheit", "wind_speed_unit": "mph", "precipitation_unit": "inch"} {
		if got := query.Get(parameter); got != want {
			t.Fatalf("%s = %q, want %q", parameter, got, want)
		}
	}
	// A metric lookup asks for none of them.
	metric := mediaQueryOf(t, fake.seen[len(fake.seen)-2])
	if metric.Get("temperature_unit") != "" {
		t.Fatalf("metric query = %v", metric)
	}
	if got := metric.Get("forecast_days"); got != "3" {
		t.Fatalf("forecast_days = %q", got)
	}
}

// TestWeatherReportsAnUnknownPlace pins the message.
func TestWeatherReportsAnUnknownPlace(t *testing.T) {
	geocoding := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"results": []any{}})
	})
	previous := mediaOpenMeteoGeocoding
	mediaOpenMeteoGeocoding = geocoding + "/search"
	t.Cleanup(func() { mediaOpenMeteoGeocoding = previous })
	adapter := mediaWeatherAdapter(t, "open_meteo", "https://example.test/v1")
	_, err := adapter.Weather(context.Background(), providers.WeatherRequest{Location: "Atlantis", Current: true})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindBadRequest {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != `No place called "Atlantis" was found.` {
		t.Fatalf("message = %q", got)
	}
}

// TestWeatherValidateLooksUpFixedCoordinates: one call of quota, and only a
// refusal of the key is a definite verdict.
func TestWeatherValidateLooksUpFixedCoordinates(t *testing.T) {
	fake := &mediaWeatherFake{answers: map[string]any{
		"/v1/forecast": map[string]any{"current": map[string]any{"temperature_2m": 11}},
	}}
	base := mediaTestServer(t, fake.handler(t))
	adapter := mediaWeatherAdapter(t, "open_meteo", base+"/v1")
	validation, err := adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validation.Status != providers.ValidationHealthy {
		t.Fatalf("validation = %+v", validation)
	}
	query := mediaQueryOf(t, fake.seen[0])
	if query.Get("latitude") != "52.52" || query.Get("longitude") != "13.41" {
		t.Fatalf("query = %v", query)
	}

	refusing := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	})
	invalid, err := mediaWeatherAdapter(t, "open_meteo", refusing+"/v1").Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if invalid.Status != providers.ValidationInvalid || invalid.Message == nil {
		t.Fatalf("validation = %+v", invalid)
	}

	broken := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":true,"reason":"weather exploded"}`))
	})
	unknown, err := mediaWeatherAdapter(t, "open_meteo", broken+"/v1").Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if unknown.Status != providers.ValidationUnknown {
		t.Fatalf("validation = %+v", unknown)
	}
}

// TestWeatherAdapterSurface: the methods a weather service serves.
func TestWeatherAdapterSurface(t *testing.T) {
	adapter := mediaWeatherAdapter(t, "open_meteo", "https://example.test/v1")
	if !adapter.Supports(providers.CapWeather) || adapter.Supports(providers.CapChat) || adapter.Supports(providers.CapSearch) {
		t.Fatal("a weather service serves weather only")
	}
	if !adapter.TranscribeTranslate() || !adapter.TranscribeTimestamps() {
		t.Fatal("the default for a never-transcribing adapter is true")
	}
	if got := adapter.serviceName(); got != "open_meteo" {
		t.Fatalf("service = %q", got)
	}
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	if len(models) != 1 || models[0].Kind != providers.KindWeather {
		t.Fatalf("models = %+v", models)
	}
	if quota, err := adapter.FetchQuota(context.Background()); err != nil || quota != nil {
		t.Fatalf("quota = %v %v", quota, err)
	}
	// A definition that names no service is refused without an upstream call.
	unknown := NewWeatherAdapter(providers.Definition{ID: "custom"}, mediaTestDeps("k", "", nil))
	_, err = unknown.Weather(context.Background(), providers.WeatherRequest{Location: "x", Current: true})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindBadRequest {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "Unknown weather service ." {
		t.Fatalf("message = %q", got)
	}
}
