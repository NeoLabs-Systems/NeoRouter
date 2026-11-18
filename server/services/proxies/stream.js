'use strict';

// Keep a routing lease for the entire response, including SSE. Read on demand
// so large downloads and long streams never accumulate a second body buffer.
function withLease(response, release, failed) {
  if (!response.body) { release(); return response; }
  const reader = response.body.getReader();
  let finished = false;
  function finish() {
    if (finished) return;
    finished = true;
    reader.releaseLock();
    release();
  }
  const body = new ReadableStream({
    async pull(controller) {
      try {
        const { done, value } = await reader.read();
        if (done) { finish(); controller.close(); } else controller.enqueue(value);
      } catch (error) {
        if (!finished) failed();
        finish();
        controller.error(error);
      }
    },
    async cancel(reason) {
      try { await reader.cancel(reason); } finally { finish(); }
    },
  }, { highWaterMark: 0 });
  const wrapped = new Response(body, { status: response.status, statusText: response.statusText, headers: response.headers });
  for (const key of ['url', 'redirected', 'type']) Object.defineProperty(wrapped, key, { value: response[key] });
  return wrapped;
}
module.exports = { withLease };
