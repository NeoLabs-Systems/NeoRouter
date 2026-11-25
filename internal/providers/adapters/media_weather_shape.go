package adapters

import (
	"math"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// MediaWeatherPlace is the resolved place a forecast is for.
type MediaWeatherPlace struct {
	Name      any `json:"name"`
	Latitude  any `json:"latitude"`
	Longitude any `json:"longitude"`
	Timezone  any `json:"timezone"`
}

// MediaWeatherCurrent is the current-conditions block.
type MediaWeatherCurrent struct {
	Time          any `json:"time"`
	Temperature   any `json:"temperature"`
	FeelsLike     any `json:"feels_like"`
	Humidity      any `json:"humidity"`
	WindSpeed     any `json:"wind_speed"`
	WindDirection any `json:"wind_direction"`
	Precipitation any `json:"precipitation"`
	Condition     any `json:"condition"`
}

// MediaWeatherHour is one hourly step.
type MediaWeatherHour struct {
	Time                     any `json:"time"`
	Temperature              any `json:"temperature"`
	PrecipitationProbability any `json:"precipitation_probability"`
	Precipitation            any `json:"precipitation"`
	WindSpeed                any `json:"wind_speed"`
	Condition                any `json:"condition"`
}

// MediaWeatherDay is one daily step.
type MediaWeatherDay struct {
	Date                     any `json:"date"`
	TemperatureMax           any `json:"temperature_max"`
	TemperatureMin           any `json:"temperature_min"`
	Precipitation            any `json:"precipitation"`
	PrecipitationProbability any `json:"precipitation_probability"`
	Condition                any `json:"condition"`
	Sunrise                  any `json:"sunrise"`
	Sunset                   any `json:"sunset"`
}

// mediaWMO are the WMO weather interpretation codes Open-Meteo reports.
var mediaWMO = map[int]string{
	0: "Clear sky", 1: "Mainly clear", 2: "Partly cloudy", 3: "Overcast", 45: "Fog", 48: "Rime fog",
	51: "Light drizzle", 53: "Drizzle", 55: "Dense drizzle", 56: "Freezing drizzle", 57: "Freezing drizzle",
	61: "Light rain", 63: "Rain", 65: "Heavy rain", 66: "Freezing rain", 67: "Freezing rain",
	71: "Light snow", 73: "Snow", 75: "Heavy snow", 77: "Snow grains", 80: "Light showers", 81: "Showers", 82: "Violent showers",
	85: "Snow showers", 86: "Heavy snow showers", 95: "Thunderstorm", 96: "Thunderstorm with hail", 99: "Thunderstorm with hail",
}

// mediaTomorrowCodes are Tomorrow.io's weather codes.
var mediaTomorrowCodes = map[int]string{
	1000: "Clear", 1100: "Mostly clear", 1101: "Partly cloudy", 1102: "Mostly cloudy", 1001: "Cloudy", 2000: "Fog", 2100: "Light fog",
	4000: "Drizzle", 4001: "Rain", 4200: "Light rain", 4201: "Heavy rain", 5000: "Snow", 5001: "Flurries", 5100: "Light snow", 5101: "Heavy snow",
	6000: "Freezing drizzle", 6001: "Freezing rain", 6200: "Light freezing rain", 6201: "Heavy freezing rain",
	7000: "Ice pellets", 7101: "Heavy ice pellets", 7102: "Light ice pellets", 8000: "Thunderstorm",
}

// mediaWeatherRound is the JS round(value, digits = 1): null for a value that is
// not a number, else the number rounded the way Math.round rounds.
func mediaWeatherRound(value any, digits int) any {
	number := providers.ToNumber(value)
	if number == nil {
		return nil
	}
	scale := math.Pow(10, float64(digits))
	return math.Floor(*number*scale+0.5) / scale
}

// mediaWeatherValue is a member that must reach the client as a number or null.
func mediaWeatherValue(value any) any {
	number := providers.ToNumber(value)
	if number == nil {
		return nil
	}
	return *number
}

// mediaWeatherText is a member that must reach the client as text or null.
func mediaWeatherText(value any) any {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return nil
}

// mediaWeatherCondition reads one of the code tables.
func mediaWeatherCondition(codes map[int]string, value any) any {
	number := providers.ToNumber(value)
	if number == nil {
		return nil
	}
	if text, found := codes[int(*number)]; found {
		return text
	}
	return nil
}

// mediaWindSpeed converts metres per second into the requested unit.
func mediaWindSpeed(value any, units string) any {
	number := providers.ToNumber(value)
	if number == nil {
		return nil
	}
	if units == "imperial" {
		return mediaWeatherRound(*number*2.23694, 1)
	}
	return mediaWeatherRound(*number*3.6, 1)
}

// mediaRainfall converts millimetres into the requested unit.
func mediaRainfall(millimetres any, units string) any {
	number := providers.ToNumber(millimetres)
	if number == nil {
		return nil
	}
	if units == "imperial" {
		return mediaWeatherRound(*number/25.4, 2)
	}
	return mediaWeatherRound(*number, 1)
}

// mediaISOUTC is a Unix time as the ISO instant the JS forecast blocks carry.
func mediaISOUTC(seconds any) any {
	number := providers.ToNumber(seconds)
	if number == nil {
		return nil
	}
	return time.Unix(int64(math.Floor(*number)), 0).UTC().Format("2006-01-02T15:04:05Z")
}

// mediaUTCDate is a Unix time as the YYYY-MM-DD day of a 3-hour step: the
// service's own offset decides which day a step belongs to.
func mediaUTCDate(seconds any) string {
	number := providers.ToNumber(seconds)
	if number == nil {
		return ""
	}
	return time.Unix(int64(math.Floor(*number)), 0).UTC().Format("2006-01-02")
}

// mediaCoords is a location as the text a coordinate-taking service expects.
func mediaCoords(location any) string {
	if fields := mediaObject(location); fields != nil {
		return mediaString(fields["latitude"]) + "," + mediaString(fields["longitude"])
	}
	return mediaString(location)
}

// mediaPlaceName joins the parts of a place name, dropping the missing ones.
func mediaPlaceName(parts ...any) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.(string); ok && text != "" {
			kept = append(kept, text)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, ", ")
}

// mediaPlaceNameOrNil is mediaPlaceName for a block that reports null instead
// of an empty name.
func mediaPlaceNameOrNil(parts ...any) any {
	if name := mediaPlaceName(parts...); name != "" {
		return name
	}
	return nil
}
