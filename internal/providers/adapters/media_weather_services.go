package adapters

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// weatherWeatherAPI is WeatherAPI.com's forecast: imperial fields sit beside the
// metric ones, and the request picks which to read.
func weatherWeatherAPI(ctx context.Context, adapter *WeatherAdapter, params mediaWeatherParams, call providers.Call) (*providers.WeatherReport, error) {
	query := url.Values{
		"key": {adapter.secret}, "q": {mediaCoords(params.Location)},
		"days": {strconv.Itoa(params.Days)}, "aqi": {"no"}, "alerts": {"no"},
	}
	response, err := adapter.doGet(ctx, "/forecast.json?"+query.Encode(), call)
	if err != nil {
		return nil, err
	}
	imperial := params.Units == "imperial"
	location := mediaMember(response.Data, "location")
	forecast := mediaList(mediaMember(mediaMember(response.Data, "forecast"), "forecastday"))
	report := &providers.WeatherReport{
		Location: MediaWeatherPlace{
			Name:      mediaPlaceNameOrNil(mediaMember(location, "name"), mediaMember(location, "region"), mediaMember(location, "country")),
			Latitude:  mediaWeatherValue(mediaMember(location, "lat")),
			Longitude: mediaWeatherValue(mediaMember(location, "lon")),
			Timezone:  mediaWeatherText(mediaMember(location, "tz_id")),
		},
		Headers: response.Headers,
	}
	if current := mediaObject(mediaMember(response.Data, "current")); params.Current && current != nil {
		report.Current = MediaWeatherCurrent{
			Time:          mediaWeatherText(strings.ReplaceAll(mediaString(current["last_updated"]), " ", "T")),
			Temperature:   mediaChoice(imperial, current["temp_f"], current["temp_c"]),
			FeelsLike:     mediaChoice(imperial, current["feelslike_f"], current["feelslike_c"]),
			Humidity:      mediaWeatherValue(current["humidity"]),
			WindSpeed:     mediaChoice(imperial, current["wind_mph"], current["wind_kph"]),
			WindDirection: mediaWeatherValue(current["wind_degree"]),
			Precipitation: mediaChoice(imperial, current["precip_in"], current["precip_mm"]),
			Condition:     mediaWeatherText(mediaMember(mediaMember(current, "condition"), "text")),
		}
	}
	if params.Hourly {
		hours := make([]MediaWeatherHour, 0, len(forecast))
		for _, day := range forecast {
			for _, item := range mediaList(mediaMember(day, "hour")) {
				hours = append(hours, MediaWeatherHour{
					Time:                     strings.ReplaceAll(mediaString(mediaMember(item, "time")), " ", "T"),
					Temperature:              mediaChoice(imperial, mediaMember(item, "temp_f"), mediaMember(item, "temp_c")),
					PrecipitationProbability: mediaWeatherChance(mediaMember(item, "chance_of_rain"), mediaMember(item, "chance_of_snow")),
					Precipitation:            mediaChoice(imperial, mediaMember(item, "precip_in"), mediaMember(item, "precip_mm")),
					WindSpeed:                mediaChoice(imperial, mediaMember(item, "wind_mph"), mediaMember(item, "wind_kph")),
					Condition:                mediaWeatherText(mediaMember(mediaMember(item, "condition"), "text")),
				})
			}
		}
		report.Hourly = hours
	}
	if params.Daily {
		days := make([]MediaWeatherDay, 0, len(forecast))
		for _, item := range forecast {
			day := mediaMember(item, "day")
			days = append(days, MediaWeatherDay{
				Date:                     mediaWeatherText(mediaMember(item, "date")),
				TemperatureMax:           mediaChoice(imperial, mediaMember(day, "maxtemp_f"), mediaMember(day, "maxtemp_c")),
				TemperatureMin:           mediaChoice(imperial, mediaMember(day, "mintemp_f"), mediaMember(day, "mintemp_c")),
				Precipitation:            mediaChoice(imperial, mediaMember(day, "totalprecip_in"), mediaMember(day, "totalprecip_mm")),
				PrecipitationProbability: mediaWeatherChance(mediaMember(day, "daily_chance_of_rain"), mediaMember(day, "daily_chance_of_snow")),
				Condition:                mediaWeatherText(mediaMember(mediaMember(day, "condition"), "text")),
				Sunrise:                  mediaWeatherText(mediaMember(mediaMember(item, "astro"), "sunrise")),
				Sunset:                   mediaWeatherText(mediaMember(mediaMember(item, "astro"), "sunset")),
			})
		}
		report.Daily = days
	}
	return report, nil
}

// mediaChoice is the imperial member when imperial units were asked for, the
// metric one otherwise.
func mediaChoice(imperial bool, imperialValue, metricValue any) any {
	if imperial {
		return mediaWeatherValue(imperialValue)
	}
	return mediaWeatherValue(metricValue)
}

// mediaWeatherChance is the larger of a rain and a snow chance.
func mediaWeatherChance(rain, snow any) any {
	chance := 0.0
	for _, value := range []any{rain, snow} {
		if number := providers.ToNumber(value); number != nil && *number > chance {
			chance = *number
		}
	}
	return chance
}

// weatherTomorrow is Tomorrow.io's realtime and forecast timelines.
func weatherTomorrow(ctx context.Context, adapter *WeatherAdapter, params mediaWeatherParams, call providers.Call) (*providers.WeatherReport, error) {
	query := url.Values{
		"location": {mediaCoords(params.Location)}, "units": {params.Units}, "apikey": {adapter.secret},
	}
	var headers http.Header
	var now map[string]any
	var place map[string]any
	if params.Current {
		response, err := adapter.doGet(ctx, "/weather/realtime?"+query.Encode(), call)
		if err != nil {
			return nil, err
		}
		headers = response.Headers
		now = mediaObject(response.Data)
		place = mediaObject(mediaMember(response.Data, "location"))
	}
	var timelines map[string]any
	if params.Hourly || params.Daily {
		steps := make([]string, 0, 2)
		if params.Hourly {
			steps = append(steps, "1h")
		}
		if params.Daily {
			steps = append(steps, "1d")
		}
		response, err := adapter.doGet(ctx, "/weather/forecast?"+query.Encode()+"&timesteps="+strings.Join(steps, ","), call)
		if err != nil {
			return nil, err
		}
		headers = response.Headers
		timelines = mediaObject(mediaMember(response.Data, "timelines"))
		if location := mediaObject(mediaMember(response.Data, "location")); location != nil {
			place = location
		}
	}
	// Metric wind speed comes in m/s.
	wind := func(value any) any {
		number := providers.ToNumber(value)
		if number == nil {
			return nil
		}
		if params.Units == "imperial" {
			return mediaWeatherRound(*number, 1)
		}
		return mediaWeatherRound(*number*3.6, 1)
	}
	values := mediaObject(mediaMember(now, "data"))
	values = mediaObject(mediaMember(values, "values"))
	name := mediaWeatherText(mediaMember(place, "name"))
	if name == nil {
		if text, ok := params.Location.(string); ok {
			name = text
		}
	}
	location := MediaWeatherPlace{
		Name:      name,
		Latitude:  mediaFirstNonNull(mediaWeatherValue(mediaMember(place, "lat")), mediaWeatherValue(mediaMember(objectOf(params.Location), "latitude"))),
		Longitude: mediaFirstNonNull(mediaWeatherValue(mediaMember(place, "lon")), mediaWeatherValue(mediaMember(objectOf(params.Location), "longitude"))),
	}
	report := &providers.WeatherReport{Location: location, Headers: headers}
	if values != nil {
		report.Current = MediaWeatherCurrent{
			Time:          mediaWeatherText(mediaMember(mediaMember(now, "data"), "time")),
			Temperature:   mediaWeatherValue(values["temperature"]),
			FeelsLike:     mediaWeatherValue(values["temperatureApparent"]),
			Humidity:      mediaWeatherValue(values["humidity"]),
			WindSpeed:     wind(values["windSpeed"]),
			WindDirection: mediaWeatherValue(values["windDirection"]),
			Precipitation: mediaWeatherValue(values["rainIntensity"]),
			Condition:     mediaWeatherCondition(mediaTomorrowCodes, values["weatherCode"]),
		}
	}
	if params.Hourly && timelines != nil {
		hours := make([]MediaWeatherHour, 0, len(mediaList(timelines["hourly"])))
		for _, item := range mediaList(timelines["hourly"]) {
			step := mediaMember(item, "values")
			hours = append(hours, MediaWeatherHour{
				Time:                     mediaWeatherText(mediaMember(item, "time")),
				Temperature:              mediaWeatherValue(mediaMember(step, "temperature")),
				PrecipitationProbability: mediaWeatherValue(mediaMember(step, "precipitationProbability")),
				Precipitation:            mediaWeatherValue(mediaMember(step, "rainIntensity")),
				WindSpeed:                wind(mediaMember(step, "windSpeed")),
				Condition:                mediaWeatherCondition(mediaTomorrowCodes, mediaMember(step, "weatherCode")),
			})
		}
		report.Hourly = hours
	}
	if params.Daily && timelines != nil {
		days := make([]MediaWeatherDay, 0, len(mediaList(timelines["daily"])))
		for _, item := range mediaList(timelines["daily"]) {
			if len(days) >= params.Days {
				break
			}
			step := mediaMember(item, "values")
			date := mediaWeatherText(mediaSliceUTF(mediaString(mediaMember(item, "time")), 10))
			days = append(days, MediaWeatherDay{
				Date:                     date,
				TemperatureMax:           mediaWeatherValue(mediaMember(step, "temperatureMax")),
				TemperatureMin:           mediaWeatherValue(mediaMember(step, "temperatureMin")),
				Precipitation:            mediaWeatherValue(mediaMember(step, "rainAccumulationSum")),
				PrecipitationProbability: mediaWeatherValue(mediaMember(step, "precipitationProbabilityMax")),
				Condition:                mediaWeatherCondition(mediaTomorrowCodes, mediaMember(step, "weatherCodeMax")),
				Sunrise:                  mediaWeatherText(mediaMember(step, "sunriseTime")),
				Sunset:                   mediaWeatherText(mediaMember(step, "sunsetTime")),
			})
		}
		report.Daily = days
	}
	return report, nil
}

// objectOf reads a location that may be a place name instead of coordinates.
func objectOf(location any) map[string]any {
	if fields := mediaObject(location); fields != nil {
		return fields
	}
	return nil
}

// weatherVisualCrossing is Visual Crossing's timeline API.
func weatherVisualCrossing(ctx context.Context, adapter *WeatherAdapter, params mediaWeatherParams, call providers.Call) (*providers.WeatherReport, error) {
	include := make([]string, 0, 3)
	if params.Current {
		include = append(include, "current")
	}
	if params.Hourly {
		include = append(include, "hours")
	}
	if params.Daily || params.Hourly {
		include = append(include, "days")
	}
	unitGroup := "metric"
	if params.Units == "imperial" {
		unitGroup = "us"
	}
	query := url.Values{
		"key": {adapter.secret}, "unitGroup": {unitGroup},
		"include": {strings.Join(include, ",")}, "contentType": {"json"},
	}
	path := "/timeline/" + mediaEncodeURIComponent(mediaCoords(params.Location)) +
		"/next" + strconv.Itoa(params.Days-1) + "days?" + query.Encode()
	response, err := adapter.doGet(ctx, path, call)
	if err != nil {
		return nil, err
	}
	current := mediaObject(mediaMember(response.Data, "currentConditions"))
	list := mediaList(mediaMember(response.Data, "days"))
	report := &providers.WeatherReport{
		Location: MediaWeatherPlace{
			Name:      mediaWeatherText(mediaMember(response.Data, "resolvedAddress")),
			Latitude:  mediaWeatherValue(mediaMember(response.Data, "latitude")),
			Longitude: mediaWeatherValue(mediaMember(response.Data, "longitude")),
			Timezone:  mediaWeatherText(mediaMember(response.Data, "timezone")),
		},
		Headers: response.Headers,
	}
	if params.Current && current != nil {
		today := ""
		if len(list) > 0 {
			today = mediaString(mediaMember(list[0], "datetime"))
		}
		report.Current = MediaWeatherCurrent{
			Time:          today + "T" + mediaString(current["datetime"]),
			Temperature:   mediaWeatherValue(current["temp"]),
			FeelsLike:     mediaWeatherValue(current["feelslike"]),
			Humidity:      mediaWeatherValue(current["humidity"]),
			WindSpeed:     mediaWeatherValue(current["windspeed"]),
			WindDirection: mediaWeatherValue(current["winddir"]),
			Precipitation: mediaWeatherZero(current["precip"]),
			Condition:     mediaWeatherText(current["conditions"]),
		}
	}
	if params.Hourly {
		hours := make([]MediaWeatherHour, 0, len(list))
		for _, day := range list {
			date := mediaString(mediaMember(day, "datetime"))
			for _, item := range mediaList(mediaMember(day, "hours")) {
				hours = append(hours, MediaWeatherHour{
					Time:                     date + "T" + mediaSliceUTF(mediaString(mediaMember(item, "datetime")), 5),
					Temperature:              mediaWeatherValue(mediaMember(item, "temp")),
					PrecipitationProbability: mediaWeatherValue(mediaMember(item, "precipprob")),
					Precipitation:            mediaWeatherZero(mediaMember(item, "precip")),
					WindSpeed:                mediaWeatherValue(mediaMember(item, "windspeed")),
					Condition:                mediaWeatherText(mediaMember(item, "conditions")),
				})
			}
		}
		report.Hourly = hours
	}
	if params.Daily {
		days := make([]MediaWeatherDay, 0, len(list))
		for _, item := range list {
			if len(days) >= params.Days {
				break
			}
			days = append(days, MediaWeatherDay{
				Date:                     mediaWeatherText(mediaMember(item, "datetime")),
				TemperatureMax:           mediaWeatherValue(mediaMember(item, "tempmax")),
				TemperatureMin:           mediaWeatherValue(mediaMember(item, "tempmin")),
				Precipitation:            mediaWeatherZero(mediaMember(item, "precip")),
				PrecipitationProbability: mediaWeatherValue(mediaMember(item, "precipprob")),
				Condition:                mediaWeatherText(mediaMember(item, "conditions")),
				Sunrise:                  mediaWeatherText(mediaMember(item, "sunrise")),
				Sunset:                   mediaWeatherText(mediaMember(item, "sunset")),
			})
		}
		report.Daily = days
	}
	return report, nil
}

// mediaWeatherZero is a member that reports 0 when it is missing.
func mediaWeatherZero(value any) any {
	if number := providers.ToNumber(value); number != nil {
		return *number
	}
	return 0
}
