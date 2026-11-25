package status

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// browserHarness drives analytics_client.js the way test/unit/analytics.test.js
// does, in Node's vm: the same fake window, document and XHR, the same
// assertions. It prints a JSON summary of what the script sent.
const browserHarness = `
'use strict';
const vm = require('node:vm');
const fs = require('node:fs');
const assert = require('node:assert/strict');

const source = fs.readFileSync(process.argv[2], 'utf8');
const endpoint = process.argv[3];
const appKey = process.argv[4];

class XHR {
  open() {}
  send() { this.status = 503; this.done(); }
  addEventListener(_name, fn) { this.done = fn; }
}

function recorder(sent) {
  return (url, options) => {
    if (url === endpoint) {
      assert.equal(options.method, 'POST');
      assert.equal(options.mode, 'cors');
      assert.equal(options.redirect, 'error');
      assert.equal(options.credentials, 'omit');
      assert.equal(options.referrerPolicy, 'no-referrer');
      assert.equal(options.body.get('device_id'), 'aggregate-browser');
      assert.equal(options.body.get('app_key'), appKey);
      assert.equal(options.body.has('begin_session'), false);
      assert.equal(options.body.has('metrics'), false);
      sent.push(...JSON.parse(options.body.get('events')));
    }
    return Promise.resolve({ status: 200 });
  };
}

function scenario(options) {
  const sent = [];
  const listeners = {};
  const timers = [];
  const document = {
    readyState: 'complete',
    visibilityState: 'hidden',
    addEventListener(name, fn) { listeners[name] = fn; },
  };
  const window = options.withXHR
    ? { fetch: recorder(sent), XMLHttpRequest: XHR, addEventListener() {} }
    : { fetch: recorder(sent), addEventListener(name, fn) { listeners[name] = fn; } };
  const context = {
    window,
    document,
    navigator: options.navigator || {},
    location: new URL(options.url),
    performance: options.performance || { getEntriesByType: () => [{ loadEventEnd: 1500 }] },
    URL,
    URLSearchParams,
    setInterval(fn, ms) { timers.push({ fn, ms }); },
    setTimeout(fn) { fn(); },
    WeakMap,
    Promise,
  };
  if (options.withXHR) context.XMLHttpRequest = XHR;
  vm.runInNewContext(source, context);
  return { sent, listeners, timers, document };
}

const summary = {};

// Coarse page reporting, privacy defaults, and the visible-tab heartbeat.
const app = scenario({ url: 'https://example.org/app/private-id?token=secret', navigator: { doNotTrack: '0' } });
if (app.listeners.click) app.listeners.click();
if (app.listeners.error) app.listeners.error({ message: 'secret error' });
if (app.listeners.visibilitychange) app.listeners.visibilitychange();
const heartbeat = app.timers.find((timer) => timer.ms === 60000);
assert.ok(heartbeat, 'the heartbeat timer is missing');
heartbeat.fn();
assert.equal(app.sent.filter((event) => event.key === 'active-browser').length, 0);
app.document.visibilityState = 'visible';
heartbeat.fn();
assert.equal(app.sent.filter((event) => event.key === 'active-browser').length, 1);
assert.ok(app.sent.length >= 4, 'expected coarse events, got ' + app.sent.length);
assert.deepEqual(app.sent.filter((event) => event.key === '[CLY]_view'), [{
  key: '[CLY]_view',
  count: 1,
  segmentation: { name: 'app', view: '/app', visit: 1 },
}]);
assert.ok(app.sent.some((event) => event.key === 'page-load'));
assert.ok(!app.sent.some((event) => event.key === 'page-view'));
assert.doesNotMatch(JSON.stringify(app.sent), /secret|private|token|referrer|hostname/);
summary.appEvents = app.sent.length;

const landing = scenario({ url: 'https://example.org/?token=secret', navigator: { doNotTrack: '0' } });
assert.deepEqual(landing.sent.find((event) => event.key === '[CLY]_view').segmentation, {
  name: 'landing', view: '/', visit: 1,
});

summary.doNotTrack = scenario({ url: 'https://example.org/app/', navigator: { doNotTrack: '1' } }).sent.length;
summary.globalPrivacyControl = scenario({ url: 'https://example.org/app/', navigator: { globalPrivacyControl: true } }).sent.length;
assert.equal(summary.doNotTrack, 0);
assert.equal(summary.globalPrivacyControl, 0);

// Feature counts: only fixed labels, never paths, ids or bodies.
const groups = source.match(/var groups = \['([^']+)'/);
assert.ok(groups, 'the feature group list is missing');
const group = groups[1];
const features = scenario({
  url: 'https://example.org/app/',
  withXHR: true,
  navigator: {},
  performance: { getEntriesByType: () => [] },
});
const request = new XHR();
request.open('POST', '/api/v1/' + group + '/secret-id?token=secret');
request.send('secret body');
const secondRequest = new XHR();
secondRequest.open('POST', '/api/v1/' + group + '/another-secret');
secondRequest.send();
const admin = new XHR();
admin.open('PATCH', '/api/v1/admin/users/sensitive-user-id?query=secret');
admin.send('private settings');
const unknownAdmin = new XHR();
unknownAdmin.open('GET', '/api/v1/admin/sensitive-user-id');
unknownAdmin.send();
const unrelated = new XHR();
unrelated.open('GET', 'https://outside.example/api/v1/' + group);
unrelated.send();
features.listeners.visibilitychange();
const featureEvent = features.sent.find((item) => item.key === 'feature-requests');
assert.equal(featureEvent.segmentation.feature, group);
assert.equal(featureEvent.segmentation.operation, 'write');
assert.equal(featureEvent.segmentation.outcome, 'server-error');
assert.equal(featureEvent.count, 2);
const adminEvent = features.sent.find((item) => item.segmentation && item.segmentation.feature === 'admin-users');
assert.equal(adminEvent.segmentation.operation, 'write');
assert.equal(adminEvent.segmentation.outcome, 'server-error');
assert.ok(features.sent.some((item) => item.segmentation && item.segmentation.feature === 'admin'));
assert.doesNotMatch(JSON.stringify(features.sent), /secret|outside|sensitive-user-id|private settings/);
summary.featureGroup = group;
summary.featureCount = featureEvent.count;

process.stdout.write(JSON.stringify(summary));
`

// TestBrowserAnalyticsClient runs the delivered browser script under Node,
// porting the two vm cases of test/unit/analytics.test.js. The script is
// JavaScript shipped to the browser, so only a JavaScript engine can exercise
// it; the test skips where Node is not installed.
func TestBrowserAnalyticsClient(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed: the browser analytics client cannot be exercised")
	}
	dir := t.TempDir()
	client := filepath.Join(dir, "analytics.js")
	if err := os.WriteFile(client, []byte(ClientScript(DefaultConfig())), 0o600); err != nil {
		t.Fatalf("write client: %v", err)
	}
	harness := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(harness, []byte(browserHarness), 0o600); err != nil {
		t.Fatalf("write harness: %v", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(node, harness, client, CountlyEndpoint, CountlyAppKey)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("browser client harness failed: %v\n%s", err, stderr.String())
	}
	var summary struct {
		AppEvents            int    `json:"appEvents"`
		DoNotTrack           int    `json:"doNotTrack"`
		GlobalPrivacyControl int    `json:"globalPrivacyControl"`
		FeatureGroup         string `json:"featureGroup"`
		FeatureCount         int    `json:"featureCount"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("harness output %q: %v", stdout.String(), err)
	}
	if summary.AppEvents < 4 {
		t.Fatalf("app events = %d, want at least 4", summary.AppEvents)
	}
	if summary.DoNotTrack != 0 || summary.GlobalPrivacyControl != 0 {
		t.Fatalf("privacy opt-outs sent events: %+v", summary)
	}
	if summary.FeatureGroup != "providers" {
		t.Fatalf("feature group = %q, want providers", summary.FeatureGroup)
	}
	if summary.FeatureCount != 2 {
		t.Fatalf("feature count = %d, want 2", summary.FeatureCount)
	}
}
