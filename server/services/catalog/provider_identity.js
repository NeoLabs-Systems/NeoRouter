'use strict';

const registry = require('../../providers/registry');

// Several accounts with one provider (two Groq keys) are one provider: they
// share a key and a name everywhere models are shown or counted. Custom
// endpoints are one provider per address.
function providerIdentity({ definitionId, slug, label, baseUrl }) {
  const definition = registry.get(definitionId);
  const custom = !definition || /^custom|^local/.test(definitionId);
  return {
    providerKey: custom ? `${definitionId}:${baseUrl || slug}` : definitionId,
    // A custom endpoint keeps the generic label it was created with
    // ("Custom OpenAI-compatible", "… 2"); its own name says more.
    providerName: custom ? (label && !(definition && label.startsWith(definition.label)) ? label : slug) : definition.label,
  };
}

module.exports = { providerIdentity };
