'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { detectContainer } = require('../../runtime/container');

test('an explicit NEOROUTER_CONTAINER wins over everything else', () => {
  assert.equal(detectContainer({ NEOROUTER_CONTAINER: '1' }), true);
  assert.equal(detectContainer({ NEOROUTER_CONTAINER: 'true' }), true);
});

test('Kubernetes and the container runtime env are detected', () => {
  assert.equal(detectContainer({ KUBERNETES_SERVICE_HOST: '10.0.0.1' }), true);
  assert.equal(detectContainer({ container: 'oci' }), true);
  // systemd-nspawn and friends set "host"; that is not a container.
  assert.equal(detectContainer({ container: 'host' }), false);
});

test('an unset marker falls through to the runtime filesystem', () => {
  // No env marker: the answer comes from /.dockerenv and /proc/1/cgroup, so it
  // is "true" in a container and "false" on a bare host — both are correct.
  assert.equal(typeof detectContainer({}), 'boolean');
  assert.equal(detectContainer({ NEOROUTER_CONTAINER: '0' }), detectContainer({}));
});
