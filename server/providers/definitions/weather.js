'use strict';

// Weather services. Each one appears in the catalog as one "weather" model so
// routing, fallback, learning, quotas and free rules work as for any other
// kind. Free tiers and limits checked on the providers' pricing pages and
// docs, 2026-10-30.
const weather = (service, model, limits) => ({
  adapter: 'weather',
  category: 'weather',
  weather: { service },
  media: { weather: true },
  models: { static: [{ type: 'weather', ...model }] },
  limits: { pool: 'account', account: limits },
});

module.exports = [
  {
    id: 'open_meteo',
    label: 'Open-Meteo',
    description: 'Open weather forecasts from national weather services. No key needed.',
    website: 'https://open-meteo.com',
    keyUrl: 'https://open-meteo.com/en/pricing',
    // Self-hosted instances work too; paid keys use customer-api.open-meteo.com/v1.
    baseUrl: 'https://api.open-meteo.com/v1',
    editableBaseUrl: true,
    keyOptional: true,
    ...weather('open_meteo', { id: 'open-meteo-forecast', name: 'Open-Meteo Forecast' }, { rpm: 600, rpd: 10000 }),
    billing: {
      defaultMode: 'free_tier',
      freeTierAll: true,
      note: 'Free without a key for non-commercial use: 600 calls a minute, 5,000 an hour, 10,000 a day. Commercial use needs a paid plan (a key and customer-api.open-meteo.com as the base URL). Data is CC BY 4.0: credit Open-Meteo.',
      source: 'https://open-meteo.com/en/pricing',
      checkedAt: '2026-10-30',
    },
  },
  {
    id: 'openweathermap',
    label: 'OpenWeatherMap',
    description: 'Current weather and a 5-day forecast in 3-hour steps.',
    website: 'https://openweathermap.org',
    keyUrl: 'https://home.openweathermap.org/api_keys',
    baseUrl: 'https://api.openweathermap.org',
    auth: { type: 'none' },
    ...weather('openweathermap', { id: 'openweathermap-forecast', name: 'OpenWeatherMap Forecast' }, { rpm: 60 }),
    billing: {
      defaultMode: 'free_tier',
      freeTierAll: true,
      note: 'Free plan: 60 calls a minute and 1,000,000 a month for current weather, the 5-day forecast and geocoding. A place name costs one extra geocoding call. Requests fail beyond the limits.',
      source: 'https://openweathermap.org/price',
      checkedAt: '2026-10-30',
    },
  },
  {
    id: 'weatherapi',
    label: 'WeatherAPI.com',
    description: 'Current weather and forecasts by place name or coordinates.',
    website: 'https://www.weatherapi.com',
    keyUrl: 'https://www.weatherapi.com/my/',
    baseUrl: 'https://api.weatherapi.com/v1',
    auth: { type: 'none' },
    ...weather('weatherapi', { id: 'weatherapi-forecast', name: 'WeatherAPI Forecast' }, { rpm: 60 }),
    billing: {
      defaultMode: 'free_tier',
      freeTierAll: true,
      note: 'Free plan: 100,000 calls a month with a 3-day forecast. Beyond that no data until the next month: it never bills automatically.',
      source: 'https://www.weatherapi.com/pricing.aspx',
      checkedAt: '2026-10-30',
    },
  },
  {
    id: 'tomorrow_io',
    label: 'Tomorrow.io',
    description: 'Hyperlocal forecasts with hourly and daily steps.',
    website: 'https://www.tomorrow.io/weather-api',
    keyUrl: 'https://app.tomorrow.io/development/keys',
    baseUrl: 'https://api.tomorrow.io/v4',
    auth: { type: 'none' },
    // Current weather plus a forecast is two calls.
    ...weather('tomorrow', { id: 'tomorrow-forecast', name: 'Tomorrow.io Forecast' }, { rpm: 25, rpd: 500 }),
    billing: {
      defaultMode: 'free_tier',
      freeTierAll: true,
      note: 'Free plan: 500 calls a day, 25 an hour, 3 a second. Asking for current weather and a forecast uses two calls. Requests fail beyond the limits.',
      source: 'https://support.tomorrow.io/hc/en-us/articles/20273728362644-Free-API-Plan-Rate-Limits',
      checkedAt: '2026-10-30',
    },
  },
  {
    id: 'visual_crossing',
    label: 'Visual Crossing',
    description: 'Timeline weather: current, hourly and 15-day forecasts in one call.',
    website: 'https://www.visualcrossing.com/weather-api',
    keyUrl: 'https://www.visualcrossing.com/account',
    baseUrl: 'https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services',
    auth: { type: 'none' },
    ...weather('visual_crossing', { id: 'visual-crossing-timeline', name: 'Visual Crossing Timeline', price_per_call: 0.0001 }, { rpd: 1000 }),
    billing: {
      defaultMode: 'free_tier',
      freeTierAll: true,
      note: '1,000 records a day free (one forecast for one place is one record). Without a card on file it stops there; with a card, records beyond are billed at 0.0001 USD each: choose paid for such keys.',
      source: 'https://www.visualcrossing.com/weather-data-editions',
      checkedAt: '2026-10-30',
    },
  },
];
