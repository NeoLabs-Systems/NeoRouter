package adapters

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// mediaOpenMeteoGeocoding is where Open-Meteo resolves a place name. It is a
// variable so a test can point it at a fake host; the value is the documented
// endpoint.
var mediaOpenMeteoGeocoding = "https://geocoding-api.open-meteo.com/v1/search"

// weatherOpenMeteo is Open-Meteo's forecast API, with its own geocoding for a
// place name.
func weatherOpenMeteo(ctx context.Context, adapter *WeatherAdapter, params mediaWeatherParams, call providers.Call) (*providers.WeatherReport, error) {
	place := MediaWeatherPlace{}
	if fields := mediaObject(params.Location); fields != nil {
		place.Latitude = mediaWeatherValue(fields["latitude"])
		place.Longitude = mediaWeatherValue(fields["longitude"])
	} else {
		name := mediaString(params.Location)
		query := url.Values{"name": {name}, "count": {"1"}, "format": {"json"}}
		response, err := adapter.doForeign(ctx, mediaOpenMeteoGeocoding+"?"+query.Encode(), providers.Request{Method: http.MethodGet}, call)
		if err != nil {
			return nil, err
		}
		hit := mediaFirst(mediaMember(response.Data, "results"))
		if hit == nil {
			return nil, providers.NewError(providers.KindBadRequest, `No place called "`+name+`" was found.`, providers.ErrorOptions{})
		}
		place = MediaWeatherPlace{
			Name:      mediaPlaceName(mediaMember(hit, "name"), mediaMember(hit, "admin1"), mediaMember(hit, "country")),
			Latitude:  mediaWeatherValue(mediaMember(hit, "latitude")),
			Longitude: mediaWeatherValue(mediaMember(hit, "longitude")),
		}
	}
	query := url.Values{
		"latitude":      {mediaString(place.Latitude)},
		"longitude":     {mediaString(place.Longitude)},
		"timezone":      {"auto"},
		"forecast_days": {strconv.Itoa(params.Days)},
	}
	if params.Current {
		query.Set("current", "temperature_2m,apparent_temperature,relative_humidity_2m,wind_speed_10m,wind_direction_10m,precipitation,weather_code")
	}
	if params.Hourly {
		query.Set("hourly", "temperature_2m,precipitation_probability,precipitation,wind_speed_10m,weather_code")
	}
	if params.Daily {
		query.Set("daily", "temperature_2m_max,temperature_2m_min,precipitation_sum,precipitation_probability_max,weather_code,sunrise,sunset")
	}
	if params.Units == "imperial" {
		query.Set("temperature_unit", "fahrenheit")
		query.Set("wind_speed_unit", "mph")
		query.Set("precipitation_unit", "inch")
	}
	if adapter.secret != "" {
		query.Set("apikey", adapter.secret)
	}
	response, err := adapter.doGet(ctx, "/forecast?"+query.Encode(), call)
	if err != nil {
		return nil, err
	}
	place.Timezone = mediaWeatherText(mediaMember(response.Data, "timezone"))
	current := mediaObject(mediaMember(response.Data, "current"))
	hourly := mediaObject(mediaMember(response.Data, "hourly"))
	daily := mediaObject(mediaMember(response.Data, "daily"))
	report := &providers.WeatherReport{Location: place, Headers: response.Headers}
	if current != nil {
		report.Current = MediaWeatherCurrent{
			Time:          mediaWeatherText(current["time"]),
			Temperature:   mediaWeatherValue(current["temperature_2m"]),
			FeelsLike:     mediaWeatherValue(current["apparent_temperature"]),
			Humidity:      mediaWeatherValue(current["relative_humidity_2m"]),
			WindSpeed:     mediaWeatherValue(current["wind_speed_10m"]),
			WindDirection: mediaWeatherValue(current["wind_direction_10m"]),
			Precipitation: mediaWeatherValue(current["precipitation"]),
			Condition:     mediaWeatherCondition(mediaWMO, current["weather_code"]),
		}
	}
	if hourly != nil {
		times := mediaList(hourly["time"])
		steps := make([]MediaWeatherHour, 0, len(times))
		for index, at := range times {
			steps = append(steps, MediaWeatherHour{
				Time:                     at,
				Temperature:              mediaWeatherValue(mediaAt(hourly["temperature_2m"], index)),
				PrecipitationProbability: mediaWeatherValue(mediaAt(hourly["precipitation_probability"], index)),
				Precipitation:            mediaWeatherValue(mediaAt(hourly["precipitation"], index)),
				WindSpeed:                mediaWeatherValue(mediaAt(hourly["wind_speed_10m"], index)),
				Condition:                mediaWeatherCondition(mediaWMO, mediaAt(hourly["weather_code"], index)),
			})
		}
		report.Hourly = steps
	}
	if daily != nil {
		dates := mediaList(daily["time"])
		days := make([]MediaWeatherDay, 0, len(dates))
		for index, date := range dates {
			days = append(days, MediaWeatherDay{
				Date:                     date,
				TemperatureMax:           mediaWeatherValue(mediaAt(daily["temperature_2m_max"], index)),
				TemperatureMin:           mediaWeatherValue(mediaAt(daily["temperature_2m_min"], index)),
				Precipitation:            mediaWeatherValue(mediaAt(daily["precipitation_sum"], index)),
				PrecipitationProbability: mediaWeatherValue(mediaAt(daily["precipitation_probability_max"], index)),
				Condition:                mediaWeatherCondition(mediaWMO, mediaAt(daily["weather_code"], index)),
				Sunrise:                  mediaWeatherText(mediaAt(daily["sunrise"], index)),
				Sunset:                   mediaWeatherText(mediaAt(daily["sunset"], index)),
			})
		}
		report.Daily = days
	}
	return report, nil
}

// mediaAt reads one step of a parallel array, null when the array is short.
func mediaAt(value any, index int) any {
	items := mediaList(value)
	if index < 0 || index >= len(items) {
		return nil
	}
	return items[index]
}

// weatherOpenWeatherMap is OpenWeatherMap's free 2.5 API: current weather and a
// five-day forecast in three-hour steps, folded into days of its own.
func weatherOpenWeatherMap(ctx context.Context, adapter *WeatherAdapter, params mediaWeatherParams, call providers.Call) (*providers.WeatherReport, error) {
	place := MediaWeatherPlace{}
	if fields := mediaObject(params.Location); fields != nil {
		place.Latitude = mediaWeatherValue(fields["latitude"])
		place.Longitude = mediaWeatherValue(fields["longitude"])
	} else {
		name := mediaString(params.Location)
		query := url.Values{"q": {name}, "limit": {"1"}, "appid": {adapter.secret}}
		response, err := adapter.doGet(ctx, "/geo/1.0/direct?"+query.Encode(), call)
		if err != nil {
			return nil, err
		}
		hit := mediaFirst(response.Data)
		if hit == nil {
			return nil, providers.NewError(providers.KindBadRequest, `No place called "`+name+`" was found.`, providers.ErrorOptions{})
		}
		place = MediaWeatherPlace{
			Name:      mediaPlaceName(mediaMember(hit, "name"), mediaMember(hit, "state"), mediaMember(hit, "country")),
			Latitude:  mediaWeatherValue(mediaMember(hit, "lat")),
			Longitude: mediaWeatherValue(mediaMember(hit, "lon")),
		}
	}
	query := url.Values{
		"lat": {mediaString(place.Latitude)}, "lon": {mediaString(place.Longitude)},
		"units": {"metric"}, "appid": {adapter.secret},
	}
	var headers http.Header
	var now map[string]any
	if params.Current {
		response, err := adapter.doGet(ctx, "/data/2.5/weather?"+query.Encode(), call)
		if err != nil {
			return nil, err
		}
		headers, now = response.Headers, mediaObject(response.Data)
	}
	var steps []any
	var offset any
	if now != nil {
		offset = now["timezone"]
	}
	if params.Hourly || params.Daily {
		response, err := adapter.doGet(ctx, "/data/2.5/forecast?"+query.Encode(), call)
		if err != nil {
			return nil, err
		}
		headers = response.Headers
		steps = mediaList(mediaMember(response.Data, "list"))
		if city := mediaMember(mediaMember(response.Data, "city"), "timezone"); city != nil {
			offset = city
		}
	}
	offsetSeconds := 0.0
	if number := providers.ToNumber(offset); number != nil {
		offsetSeconds = *number
	}
	// temp is the days' temperatures in the unit the request asked for.
	temp := func(celsius any) any {
		number := providers.ToNumber(celsius)
		if number == nil {
			return nil
		}
		if params.Units == "imperial" {
			return mediaWeatherRound(*number*9/5+32, 1)
		}
		return mediaWeatherRound(*number, 1)
	}
	// rain is a step's rain plus its snow, in millimetres.
	rain := func(item any) float64 {
		total := 0.0
		for _, kind := range []string{"rain", "snow"} {
			for _, window := range []string{"1h", "3h"} {
				if number := providers.ToNumber(mediaMember(mediaMember(item, kind), window)); number != nil {
					total += *number
					break
				}
			}
		}
		return total
	}
	type day struct {
		date  string
		items []any
	}
	byDay := make([]day, 0, len(steps))
	index := make(map[string]int, len(steps))
	for _, item := range steps {
		if item == nil {
			continue
		}
		stamp := offsetSeconds
		if number := providers.ToNumber(mediaMember(item, "dt")); number != nil {
			stamp += *number
		}
		date := mediaUTCDate(stamp)
		if position, found := index[date]; found {
			byDay[position].items = append(byDay[position].items, item)
			continue
		}
		index[date] = len(byDay)
		byDay = append(byDay, day{date: date, items: []any{item}})
	}
	report := &providers.WeatherReport{Headers: headers}
	place.Timezone = nil
	report.Location = place
	if now != nil {
		report.Current = MediaWeatherCurrent{
			Time:          mediaISOUTC(now["dt"]),
			Temperature:   temp(mediaMember(now["main"], "temp")),
			FeelsLike:     temp(mediaMember(now["main"], "feels_like")),
			Humidity:      mediaWeatherValue(mediaMember(now["main"], "humidity")),
			WindSpeed:     mediaWindSpeed(mediaMember(now["wind"], "speed"), params.Units),
			WindDirection: mediaWeatherValue(mediaMember(now["wind"], "deg")),
			Precipitation: mediaRainfall(rain(now), params.Units),
			Condition:     mediaWeatherText(mediaMember(mediaFirst(mediaMember(now, "weather")), "description")),
		}
	}
	if params.Hourly && steps != nil {
		hours := make([]MediaWeatherHour, 0, len(steps))
		for _, item := range steps {
			chance := mediaNumber(mediaMember(item, "pop"))
			probability := 0.0
			if chance != nil {
				probability = *chance * 100
			}
			hours = append(hours, MediaWeatherHour{
				Time:                     mediaISOUTC(mediaMember(item, "dt")),
				Temperature:              temp(mediaMember(mediaMember(item, "main"), "temp")),
				PrecipitationProbability: mediaWeatherRound(probability, 0),
				Precipitation:            mediaRainfall(rain(item), params.Units),
				WindSpeed:                mediaWindSpeed(mediaMember(mediaMember(item, "wind"), "speed"), params.Units),
				Condition:                mediaWeatherText(mediaMember(mediaFirst(mediaMember(item, "weather")), "description")),
			})
		}
		report.Hourly = hours
	}
	if params.Daily && steps != nil {
		days := make([]MediaWeatherDay, 0, len(byDay))
		for _, entry := range byDay {
			if len(days) >= params.Days {
				break
			}
			hottest, coldest, wettestChance := math.Inf(-1), math.Inf(1), 0.0
			precipitation := 0.0
			for _, item := range entry.items {
				high := providers.ToNumber(mediaFirstNonNull(mediaMember(mediaMember(item, "main"), "temp_max"), mediaMember(mediaMember(item, "main"), "temp")))
				low := providers.ToNumber(mediaFirstNonNull(mediaMember(mediaMember(item, "main"), "temp_min"), mediaMember(mediaMember(item, "main"), "temp")))
				if high != nil && *high > hottest {
					hottest = *high
				}
				if low != nil && *low < coldest {
					coldest = *low
				}
				precipitation += rain(item)
				if chance := providers.ToNumber(mediaMember(item, "pop")); chance != nil && *chance > wettestChance {
					wettestChance = *chance
				}
			}
			var high, low any
			if !math.IsInf(hottest, 0) {
				high = temp(hottest)
			}
			if !math.IsInf(coldest, 0) {
				low = temp(coldest)
			}
			middle := entry.items[len(entry.items)/2]
			days = append(days, MediaWeatherDay{
				Date:                     entry.date,
				TemperatureMax:           high,
				TemperatureMin:           low,
				Precipitation:            mediaRainfall(precipitation, params.Units),
				PrecipitationProbability: mediaWeatherRound(wettestChance*100, 0),
				Condition:                mediaWeatherText(mediaMember(mediaFirst(mediaMember(middle, "weather")), "description")),
			})
		}
		report.Daily = days
	}
	return report, nil
}
