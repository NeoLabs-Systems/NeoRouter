'use strict';

// Weather services behind one method: weather({ location, current, hourly,
// daily, units }) → one normalized forecast, whichever service answered.
// `location` is a place name or { latitude, longitude }; units are 'metric'
// (°C, km/h, mm) or 'imperial' (°F, mph, inch). Every service speaks its own
// dialect; the definition names it (`weather.service`).
const { BaseAdapter } = require('./base_adapter');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS } = require('../provider_error');

const round = (value, digits = 1) => (value === null || value === undefined || Number.isNaN(Number(value)) ? null : Math.round(Number(value) * 10 ** digits) / 10 ** digits);
const msToSpeed = (value, units) => round(units === 'imperial' ? value * 2.23694 : value * 3.6);
const mmOrInch = (mm, units) => round(units === 'imperial' ? mm / 25.4 : mm, units === 'imperial' ? 2 : 1);
const isoUtc = (seconds) => new Date(seconds * 1000).toISOString().replace('.000Z', 'Z');
const coords = (location) => (typeof location === 'object' ? `${location.latitude},${location.longitude}` : location);

// WMO weather interpretation codes (Open-Meteo).
const WMO = {
  0: 'Clear sky', 1: 'Mainly clear', 2: 'Partly cloudy', 3: 'Overcast', 45: 'Fog', 48: 'Rime fog',
  51: 'Light drizzle', 53: 'Drizzle', 55: 'Dense drizzle', 56: 'Freezing drizzle', 57: 'Freezing drizzle',
  61: 'Light rain', 63: 'Rain', 65: 'Heavy rain', 66: 'Freezing rain', 67: 'Freezing rain',
  71: 'Light snow', 73: 'Snow', 75: 'Heavy snow', 77: 'Snow grains', 80: 'Light showers', 81: 'Showers', 82: 'Violent showers',
  85: 'Snow showers', 86: 'Heavy snow showers', 95: 'Thunderstorm', 96: 'Thunderstorm with hail', 99: 'Thunderstorm with hail',
};

// Tomorrow.io weather codes.
const TOMORROW = {
  1000: 'Clear', 1100: 'Mostly clear', 1101: 'Partly cloudy', 1102: 'Mostly cloudy', 1001: 'Cloudy', 2000: 'Fog', 2100: 'Light fog',
  4000: 'Drizzle', 4001: 'Rain', 4200: 'Light rain', 4201: 'Heavy rain', 5000: 'Snow', 5001: 'Flurries', 5100: 'Light snow', 5101: 'Heavy snow',
  6000: 'Freezing drizzle', 6001: 'Freezing rain', 6200: 'Light freezing rain', 6201: 'Heavy freezing rain',
  7000: 'Ice pellets', 7101: 'Heavy ice pellets', 7102: 'Light ice pellets', 8000: 'Thunderstorm',
};

const SERVICES = {
  async open_meteo(adapter, { location, current, hourly, daily, units, days }, ctx) {
    let place = typeof location === 'object' ? { name: null, ...location } : null;
    if (!place) {
      const params = new URLSearchParams({ name: location, count: '1', format: 'json' });
      const { data } = await adapter.request(`https://geocoding-api.open-meteo.com/v1/search?${params}`, { timeoutMs: adapter.timeoutMs, ...ctx });
      const hit = data?.results?.[0];
      if (!hit) throw new ProviderError(KINDS.BAD_REQUEST, `No place called "${location}" was found.`);
      place = { name: [hit.name, hit.admin1, hit.country].filter(Boolean).join(', '), latitude: hit.latitude, longitude: hit.longitude };
    }
    const params = new URLSearchParams({ latitude: String(place.latitude), longitude: String(place.longitude), timezone: 'auto', forecast_days: String(days) });
    if (current) params.set('current', 'temperature_2m,apparent_temperature,relative_humidity_2m,wind_speed_10m,wind_direction_10m,precipitation,weather_code');
    if (hourly) params.set('hourly', 'temperature_2m,precipitation_probability,precipitation,wind_speed_10m,weather_code');
    if (daily) params.set('daily', 'temperature_2m_max,temperature_2m_min,precipitation_sum,precipitation_probability_max,weather_code,sunrise,sunset');
    if (units === 'imperial') { params.set('temperature_unit', 'fahrenheit'); params.set('wind_speed_unit', 'mph'); params.set('precipitation_unit', 'inch'); }
    if (adapter.secret) params.set('apikey', adapter.secret);
    const { data, headers } = await adapter.http(`/forecast?${params}`, ctx);
    const c = data?.current;
    const h = data?.hourly;
    const d = data?.daily;
    return {
      headers,
      location: { ...place, timezone: data?.timezone || null },
      current: c ? { time: c.time, temperature: c.temperature_2m, feels_like: c.apparent_temperature, humidity: c.relative_humidity_2m, wind_speed: c.wind_speed_10m, wind_direction: c.wind_direction_10m, precipitation: c.precipitation, condition: WMO[c.weather_code] || null } : null,
      hourly: h ? h.time.map((time, i) => ({ time, temperature: h.temperature_2m[i], precipitation_probability: h.precipitation_probability?.[i] ?? null, precipitation: h.precipitation[i], wind_speed: h.wind_speed_10m[i], condition: WMO[h.weather_code[i]] || null })) : null,
      daily: d ? d.time.map((date, i) => ({ date, temperature_max: d.temperature_2m_max[i], temperature_min: d.temperature_2m_min[i], precipitation: d.precipitation_sum[i], precipitation_probability: d.precipitation_probability_max?.[i] ?? null, condition: WMO[d.weather_code[i]] || null, sunrise: d.sunrise[i], sunset: d.sunset[i] })) : null,
    };
  },

  // The free 2.5 endpoints: current weather and a 5-day forecast in 3-hour steps.
  async openweathermap(adapter, { location, current, hourly, daily, units, days }, ctx) {
    const key = { appid: adapter.secret || '' };
    let place = typeof location === 'object' ? { name: null, ...location } : null;
    if (!place) {
      const { data } = await adapter.http(`/geo/1.0/direct?${new URLSearchParams({ q: location, limit: '1', ...key })}`, ctx);
      const hit = data?.[0];
      if (!hit) throw new ProviderError(KINDS.BAD_REQUEST, `No place called "${location}" was found.`);
      place = { name: [hit.name, hit.state, hit.country].filter(Boolean).join(', '), latitude: hit.lat, longitude: hit.lon };
    }
    const query = new URLSearchParams({ lat: String(place.latitude), lon: String(place.longitude), units: 'metric', ...key });
    let headers = {};
    let now = null;
    if (current) {
      const result = await adapter.http(`/data/2.5/weather?${query}`, ctx);
      headers = result.headers;
      now = result.data;
    }
    let steps = null;
    let offset = now?.timezone ?? 0;
    if (hourly || daily) {
      const result = await adapter.http(`/data/2.5/forecast?${query}`, ctx);
      headers = result.headers;
      steps = result.data?.list || [];
      offset = result.data?.city?.timezone ?? offset;
    }
    const temp = (celsius) => round(units === 'imperial' ? celsius * 9 / 5 + 32 : celsius);
    const rain = (item) => (item?.rain?.['1h'] ?? item?.rain?.['3h'] ?? 0) + (item?.snow?.['1h'] ?? item?.snow?.['3h'] ?? 0);
    const byDay = new Map();
    for (const item of steps || []) {
      const date = new Date((item.dt + offset) * 1000).toISOString().slice(0, 10);
      if (!byDay.has(date)) byDay.set(date, []);
      byDay.get(date).push(item);
    }
    return {
      headers,
      location: { ...place, timezone: null },
      current: now ? { time: isoUtc(now.dt), temperature: temp(now.main?.temp), feels_like: temp(now.main?.feels_like), humidity: now.main?.humidity ?? null, wind_speed: msToSpeed(now.wind?.speed, units), wind_direction: now.wind?.deg ?? null, precipitation: mmOrInch(rain(now), units), condition: now.weather?.[0]?.description || null } : null,
      hourly: hourly && steps ? steps.map((item) => ({ time: isoUtc(item.dt), temperature: temp(item.main?.temp), precipitation_probability: round((item.pop ?? 0) * 100, 0), precipitation: mmOrInch(rain(item), units), wind_speed: msToSpeed(item.wind?.speed, units), condition: item.weather?.[0]?.description || null })) : null,
      daily: daily && steps ? [...byDay].slice(0, days).map(([date, items]) => ({
        date,
        temperature_max: temp(Math.max(...items.map((item) => item.main?.temp_max ?? item.main?.temp))),
        temperature_min: temp(Math.min(...items.map((item) => item.main?.temp_min ?? item.main?.temp))),
        precipitation: mmOrInch(items.reduce((sum, item) => sum + rain(item), 0), units),
        precipitation_probability: round(Math.max(...items.map((item) => item.pop ?? 0)) * 100, 0),
        condition: items[Math.floor(items.length / 2)].weather?.[0]?.description || null,
        sunrise: null,
        sunset: null,
      })) : null,
    };
  },

  async weatherapi(adapter, { location, current, hourly, daily, units, days }, ctx) {
    const params = new URLSearchParams({ key: adapter.secret || '', q: coords(location), days: String(days), aqi: 'no', alerts: 'no' });
    const { data, headers } = await adapter.http(`/forecast.json?${params}`, ctx);
    const imperial = units === 'imperial';
    const c = data?.current;
    const forecast = data?.forecast?.forecastday || [];
    return {
      headers,
      location: { name: [data?.location?.name, data?.location?.region, data?.location?.country].filter(Boolean).join(', ') || null, latitude: data?.location?.lat ?? null, longitude: data?.location?.lon ?? null, timezone: data?.location?.tz_id || null },
      current: current && c ? { time: c.last_updated?.replace(' ', 'T') || null, temperature: imperial ? c.temp_f : c.temp_c, feels_like: imperial ? c.feelslike_f : c.feelslike_c, humidity: c.humidity, wind_speed: imperial ? c.wind_mph : c.wind_kph, wind_direction: c.wind_degree, precipitation: imperial ? c.precip_in : c.precip_mm, condition: c.condition?.text || null } : null,
      hourly: hourly ? forecast.flatMap((day) => day.hour || []).map((item) => ({ time: item.time.replace(' ', 'T'), temperature: imperial ? item.temp_f : item.temp_c, precipitation_probability: Math.max(item.chance_of_rain ?? 0, item.chance_of_snow ?? 0), precipitation: imperial ? item.precip_in : item.precip_mm, wind_speed: imperial ? item.wind_mph : item.wind_kph, condition: item.condition?.text || null })) : null,
      daily: daily ? forecast.map((item) => ({ date: item.date, temperature_max: imperial ? item.day.maxtemp_f : item.day.maxtemp_c, temperature_min: imperial ? item.day.mintemp_f : item.day.mintemp_c, precipitation: imperial ? item.day.totalprecip_in : item.day.totalprecip_mm, precipitation_probability: Math.max(item.day.daily_chance_of_rain ?? 0, item.day.daily_chance_of_snow ?? 0), condition: item.day.condition?.text || null, sunrise: item.astro?.sunrise || null, sunset: item.astro?.sunset || null })) : null,
    };
  },

  async tomorrow(adapter, { location, current, hourly, daily, units, days }, ctx) {
    const params = new URLSearchParams({ location: coords(location), units, apikey: adapter.secret || '' });
    let headers = {};
    let now = null;
    let place = null;
    if (current) {
      const result = await adapter.http(`/weather/realtime?${params}`, ctx);
      headers = result.headers;
      now = result.data;
      place = result.data?.location;
    }
    let timelines = null;
    if (hourly || daily) {
      const result = await adapter.http(`/weather/forecast?${params}&timesteps=${[hourly && '1h', daily && '1d'].filter(Boolean).join(',')}`, ctx);
      headers = result.headers;
      timelines = result.data?.timelines || {};
      place = result.data?.location || place;
    }
    // Metric wind speed comes in m/s.
    const wind = (value) => (units === 'imperial' ? round(value) : round(value * 3.6));
    const v = now?.data?.values;
    return {
      headers,
      location: { name: place?.name || (typeof location === 'string' ? location : null), latitude: place?.lat ?? location.latitude ?? null, longitude: place?.lon ?? location.longitude ?? null, timezone: null },
      current: v ? { time: now.data.time, temperature: v.temperature, feels_like: v.temperatureApparent, humidity: v.humidity, wind_speed: wind(v.windSpeed), wind_direction: v.windDirection, precipitation: v.rainIntensity ?? null, condition: TOMORROW[v.weatherCode] || null } : null,
      hourly: hourly && timelines ? (timelines.hourly || []).map((item) => ({ time: item.time, temperature: item.values.temperature, precipitation_probability: item.values.precipitationProbability, precipitation: item.values.rainIntensity ?? null, wind_speed: wind(item.values.windSpeed), condition: TOMORROW[item.values.weatherCode] || null })) : null,
      daily: daily && timelines ? (timelines.daily || []).slice(0, days).map((item) => ({ date: item.time.slice(0, 10), temperature_max: item.values.temperatureMax, temperature_min: item.values.temperatureMin, precipitation: item.values.rainAccumulationSum ?? null, precipitation_probability: item.values.precipitationProbabilityMax ?? null, condition: TOMORROW[item.values.weatherCodeMax] || null, sunrise: item.values.sunriseTime || null, sunset: item.values.sunsetTime || null })) : null,
    };
  },

  async visual_crossing(adapter, { location, current, hourly, daily, units, days }, ctx) {
    const include = [current && 'current', hourly && 'hours', (daily || hourly) && 'days'].filter(Boolean).join(',');
    const params = new URLSearchParams({ key: adapter.secret || '', unitGroup: units === 'imperial' ? 'us' : 'metric', include, contentType: 'json' });
    const { data, headers } = await adapter.http(`/timeline/${encodeURIComponent(coords(location))}/next${days - 1}days?${params}`, ctx);
    const c = data?.currentConditions;
    const list = data?.days || [];
    return {
      headers,
      location: { name: data?.resolvedAddress || null, latitude: data?.latitude ?? null, longitude: data?.longitude ?? null, timezone: data?.timezone || null },
      current: current && c ? { time: `${list[0]?.datetime}T${c.datetime}`, temperature: c.temp, feels_like: c.feelslike, humidity: c.humidity, wind_speed: c.windspeed, wind_direction: c.winddir, precipitation: c.precip ?? 0, condition: c.conditions || null } : null,
      hourly: hourly ? list.flatMap((day) => (day.hours || []).map((item) => ({ time: `${day.datetime}T${item.datetime.slice(0, 5)}`, temperature: item.temp, precipitation_probability: item.precipprob, precipitation: item.precip ?? 0, wind_speed: item.windspeed, condition: item.conditions || null }))) : null,
      daily: daily ? list.slice(0, days).map((item) => ({ date: item.datetime, temperature_max: item.tempmax, temperature_min: item.tempmin, precipitation: item.precip ?? 0, precipitation_probability: item.precipprob, condition: item.conditions || null, sunrise: item.sunrise || null, sunset: item.sunset || null })) : null,
    };
  },
};

class WeatherAdapter extends BaseAdapter {
  get service() {
    return this.definition.weather?.service;
  }

  supports(method) {
    return method === 'weather';
  }

  async listModels() {
    return (this.definition.models?.static || []).map((entry) => normalizeModel(entry));
  }

  // A real current-weather lookup for fixed coordinates: one call of quota.
  async validate() {
    try {
      await this.weather({ location: { latitude: 52.52, longitude: 13.41 }, current: true, hourly: false, daily: false }, { signal: AbortSignal.timeout(20_000), timeoutMs: 20_000 });
      return { status: 'healthy', message: null };
    } catch (error) {
      if (error.kind === KINDS.AUTH || error.kind === KINDS.PAYMENT) return { status: 'invalid', message: error.message };
      return { status: 'unknown', message: error.message };
    }
  }

  async weather({ location, current = true, hourly = false, daily = true, units = 'metric', days = 3 }, { signal, timeoutMs } = {}) {
    const run = SERVICES[this.service];
    if (!run) throw new ProviderError(KINDS.BAD_REQUEST, `Unknown weather service ${this.service}.`);
    const { headers, ...result } = await run(this, { location, current, hourly, daily, units, days: Math.min(16, Math.max(1, days)) }, { signal, timeoutMs });
    // `units` is left out: runCapability reads result.units as billed units.
    return { ...result, headers };
  }
}

module.exports = { WeatherAdapter, SERVICES, WMO };
