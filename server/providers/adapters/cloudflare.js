'use strict';

const { OpenAICompatibleAdapter } = require('./openai_compatible');
const { normalizeModel } = require('../model_info');
const { KINDS } = require('../provider_error');

const API = 'https://api.cloudflare.com/client/v4';

function property(entry, id) {
  return (entry.properties || []).find((item) => item.property_id === id)?.value;
}

class CloudflareAdapter extends OpenAICompatibleAdapter {
  async validate() {
    for (const pathname of ['/user/tokens/verify', `/accounts/${encodeURIComponent(this.options.accountId || '')}/tokens/verify`]) {
      try {
        const { data } = await this.http(`${API}${pathname}`, { timeoutMs: 20_000 });
        if (data?.result?.status === 'active') return { status: 'healthy', message: null };
        return { status: 'invalid', message: `Token status: ${data?.result?.status || 'unknown'}` };
      } catch (error) {
        if (error.kind !== KINDS.AUTH) return { status: 'unknown', message: error.message };
      }
    }
    return { status: 'invalid', message: 'Cloudflare rejected this API token.' };
  }

  async listModels() {
    const { data } = await this.http(`${API}/accounts/${encodeURIComponent(this.options.accountId || '')}/ai/models/search?task=Text%20Generation&per_page=1000`, { timeoutMs: 30_000 });
    return (data?.result || []).map((entry) => normalizeModel({ id: entry.name, name: entry.name, description: entry.description }, {
      contextWindow: Number(property(entry, 'context_window')) || null,
      tools: String(property(entry, 'function_calling')) === 'true',
      vision: /vision|llava|-vl/i.test(entry.name),
    }));
  }
}

module.exports = { CloudflareAdapter };
