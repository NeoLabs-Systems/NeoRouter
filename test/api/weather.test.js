'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { getDatabase } = require('../../server/db/database');

let app;
let service;
let broken;
let session;
let secret;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const gateway = (method, path) => request(app)[method](`/v1${path}`).set('Authorization', `Bearer ${secret}`);

test.before(async () => {
  app = createApp();
  service = await createMockProvider({ models: [] });
  broken = await createMockProvider({ models: [], failWeather: true });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'forecaster', email: 'w@example.test', password: 'correct-horse-battery' })).body.session.token;
  secret = (await api('post', '/keys').send({ name: 'weather', mode: 'free_only' })).body.secret;
});

test.after(async () => {
  await service.close();
  await broken.close();
});

test('without a weather service the endpoint says so', async () => {
  assert.equal((await gateway('post', '/weather').send({ location: { latitude: 52.52, longitude: 13.41 } })).status, 503);
});

test('weather services are providers: routed, falling back and logged', async () => {
  const add = (slug, url) => api('post', '/providers/accounts').send({ definitionId: 'open_meteo', slug, baseUrl: url, billingMode: 'free_tier' });
  const first = await add('broken', broken.url);
  assert.equal(first.status, 201, JSON.stringify(first.body));
  assert.equal((await add('meteo', service.url)).status, 201);

  const kinds = (await api('get', '/models/capabilities')).body.capabilities;
  assert.ok(kinds.some((kind) => kind.id === 'weather' && kind.routable && kind.free === 2));

  for (let index = 0; index < 3; index += 1) {
    const response = await gateway('post', '/weather').send({ location: { latitude: 52.52, longitude: 13.41 }, daily: true, days: 1 });
    assert.equal(response.status, 200, JSON.stringify(response.body));
    assert.equal(response.body.object, 'weather');
    assert.equal(response.body.model, 'meteo/open-meteo-forecast');
    assert.equal(response.body.units, 'metric');
    assert.equal(response.body.location.timezone, 'Europe/Berlin');
    assert.deepEqual(response.body.current, { time: '2026-10-30T12:00', temperature: 11.2, feels_like: 9.8, humidity: 71, wind_speed: 14.4, wind_direction: 240, precipitation: 0, condition: 'Overcast' });
    assert.equal(response.body.daily[0].condition, 'Light rain');
    assert.equal(response.body.hourly, null);
  }
  const imperial = await gateway('post', '/weather').send({ location: { latitude: 52.52, longitude: 13.41 }, units: 'imperial' });
  assert.equal(imperial.status, 200);
  const sent = service.calls.filter((call) => call.kind === 'weather').at(-1).query;
  assert.equal(sent.temperature_unit, 'fahrenheit');
  assert.equal(sent.wind_speed_unit, 'mph');

  const row = getDatabase().prepare("SELECT units, is_free FROM requests WHERE capability = 'weather' AND status = 'success' LIMIT 1").get();
  assert.deepEqual(row, { units: 1, is_free: 1 });
});

test('a request must ask for something', async () => {
  const response = await gateway('post', '/weather').send({ location: 'Berlin', current: false, daily: false });
  assert.equal(response.status, 400);
});
