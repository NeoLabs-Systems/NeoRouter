'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { WeatherAdapter } = require('../../server/providers/adapters/weather');
const definitions = require('../../server/providers/definitions/weather');

// An adapter whose upstream answers from a table of path prefixes.
function fake(id, answers) {
  const definition = definitions.find((item) => item.id === id);
  const adapter = new WeatherAdapter(definition, { secret: 'k' });
  adapter.seen = [];
  adapter.http = async (pathname) => {
    adapter.seen.push(pathname);
    const match = Object.keys(answers).find((prefix) => pathname.startsWith(prefix));
    return { data: answers[match], headers: {} };
  };
  return adapter;
}

test('every weather definition validates and is free by default', () => {
  const registry = require('../../server/providers/registry');
  for (const id of ['open_meteo', 'openweathermap', 'weatherapi', 'tomorrow_io', 'visual_crossing']) {
    const definition = registry.get(id);
    assert.equal(definition.adapter, 'weather');
    assert.equal(definition.billing.defaultMode, 'free_tier');
    assert.ok(definition.billing.checkedAt);
  }
});

test('OpenWeatherMap: geocodes a name, converts m/s and folds 3-hour steps into days', async () => {
  const adapter = fake('openweathermap', {
    '/geo/1.0/direct': [{ name: 'Berlin', country: 'DE', lat: 52.5, lon: 13.4 }],
    '/data/2.5/weather': { dt: 1793361600, timezone: 3600, main: { temp: 10, feels_like: 8, humidity: 80 }, wind: { speed: 5, deg: 200 }, weather: [{ description: 'light rain' }], rain: { '1h': 0.5 } },
    '/data/2.5/forecast': { city: { timezone: 3600 }, list: [
      { dt: 1793361600, main: { temp: 10, temp_max: 11, temp_min: 9 }, wind: { speed: 2 }, pop: 0.2, weather: [{ description: 'clouds' }] },
      { dt: 1793372400, main: { temp: 12, temp_max: 13, temp_min: 12 }, wind: { speed: 2 }, pop: 0.6, rain: { '3h': 1.5 }, weather: [{ description: 'rain' }] },
    ] },
  });
  const result = await adapter.weather({ location: 'Berlin', current: true, hourly: true, daily: true });
  assert.equal(result.location.name, 'Berlin, DE');
  assert.equal(result.current.wind_speed, 18);
  assert.equal(result.current.condition, 'light rain');
  assert.equal(result.hourly.length, 2);
  assert.equal(result.daily.length, 1);
  assert.deepEqual([result.daily[0].temperature_max, result.daily[0].temperature_min, result.daily[0].precipitation_probability], [13, 9, 60]);
  assert.ok(adapter.seen.every((pathname) => pathname.includes('appid=k')));
});

test('WeatherAPI.com: picks the imperial fields', async () => {
  const adapter = fake('weatherapi', { '/forecast.json': {
    location: { name: 'Paris', country: 'France', lat: 48.8, lon: 2.3, tz_id: 'Europe/Paris' },
    current: { last_updated: '2026-10-30 12:00', temp_c: 10, temp_f: 50, feelslike_c: 9, feelslike_f: 48, humidity: 70, wind_kph: 10, wind_mph: 6.2, wind_degree: 90, precip_mm: 0, precip_in: 0, condition: { text: 'Sunny' } },
    forecast: { forecastday: [{ date: '2026-10-30', day: { maxtemp_c: 12, maxtemp_f: 53.6, mintemp_c: 5, mintemp_f: 41, totalprecip_mm: 0, totalprecip_in: 0, daily_chance_of_rain: 10, daily_chance_of_snow: 0, condition: { text: 'Sunny' } }, astro: { sunrise: '08:20 AM', sunset: '05:40 PM' }, hour: [] }] },
  } });
  const result = await adapter.weather({ location: { latitude: 48.8, longitude: 2.3 }, units: 'imperial' });
  assert.equal(result.current.temperature, 50);
  assert.equal(result.daily[0].temperature_max, 53.6);
  assert.equal(result.location.timezone, 'Europe/Paris');
  assert.match(adapter.seen[0], /q=48\.8%2C2\.3/);
});

test('Tomorrow.io and Visual Crossing map to the same shape', async () => {
  const tomorrow = fake('tomorrow_io', {
    '/weather/realtime': { data: { time: '2026-10-30T12:00:00Z', values: { temperature: 10, temperatureApparent: 9, humidity: 60, windSpeed: 5, windDirection: 180, weatherCode: 1001 } }, location: { lat: 1, lon: 2, name: 'X' } },
    '/weather/forecast': { timelines: { daily: [{ time: '2026-10-30T06:00:00Z', values: { temperatureMax: 12, temperatureMin: 4, precipitationProbabilityMax: 20, weatherCodeMax: 4001 } }] } },
  });
  const a = await tomorrow.weather({ location: 'X' });
  assert.equal(a.current.wind_speed, 18);
  assert.equal(a.current.condition, 'Cloudy');
  assert.equal(a.daily[0].condition, 'Rain');

  const visual = fake('visual_crossing', { '/timeline/': { resolvedAddress: 'Rome', latitude: 41.9, longitude: 12.5, timezone: 'Europe/Rome', currentConditions: { datetime: '12:00:00', temp: 18, feelslike: 18, humidity: 50, windspeed: 8, winddir: 10, conditions: 'Clear' }, days: [{ datetime: '2026-10-30', tempmax: 20, tempmin: 11, precip: 0, precipprob: 0, conditions: 'Clear', sunrise: '07:00:00', sunset: '17:10:00' }] } });
  const b = await visual.weather({ location: 'Rome', days: 1 });
  assert.equal(b.current.time, '2026-10-30T12:00:00');
  assert.equal(b.daily[0].temperature_max, 20);
  assert.match(visual.seen[0], /^\/timeline\/Rome\/next0days\?/);
});
